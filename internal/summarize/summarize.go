package summarize

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/rainmana/hackyfeed/internal/config"
	"github.com/rainmana/hackyfeed/internal/db"
)

type LLMConfig struct {
	APIBase string
	APIKey  string
	Model   string
}

type chatReq struct {
	Model    string `json:"model"`
	Messages []msg  `json:"messages"`
}

type msg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResp struct {
	Choices []struct {
		Message msg `json:"message"`
	} `json:"choices"`
}

type SummaryResult struct {
	Summary string `json:"summary"`
}

var readmePaths = []string{"README.md", "readme.md", "README.rst", "README", "Readme.md"}

var ErrReadmeNotFound = errors.New("repository README not found")

type llmServiceError struct {
	err error
}

func (e *llmServiceError) Error() string { return e.err.Error() }
func (e *llmServiceError) Unwrap() error { return e.err }

func Run(database *sql.DB, llm LLMConfig, cfg *config.SummarizeConfig) error {
	client := &http.Client{Timeout: 60 * time.Second}
	return runWithClient(database, llm, cfg, client)
}

func runWithClient(database *sql.DB, llm LLMConfig, cfg *config.SummarizeConfig, client *http.Client) error {
	if cfg.BatchLimit < 0 {
		return fmt.Errorf("batch_limit must be zero or greater")
	}
	if cfg.MaxReadmeChars <= 0 || cfg.MaxReadmeChars > config.MaxReadmeCharsLimit {
		return fmt.Errorf("max_readme_chars must be between 1 and %d", config.MaxReadmeCharsLimit)
	}
	repos, err := db.Unsummarized(database)
	if err != nil {
		return err
	}
	log.Printf("[summarize] %d repos need summaries", len(repos))

	if !cfg.Enabled {
		log.Println("[summarize] AI disabled, using repo descriptions as summaries")
		for _, r := range repos {
			summary := r.Description
			if summary == "" {
				summary = r.Name
			}
			if err := db.SetSummary(database, r.ID, summary); err != nil {
				return fmt.Errorf("save fallback summary for %s: %w", r.FullName, err)
			}
		}
		return nil
	}

	if cfg.BatchLimit > 0 && len(repos) > cfg.BatchLimit {
		log.Printf("[summarize] batch limit %d, processing %d of %d", cfg.BatchLimit, cfg.BatchLimit, len(repos))
		repos = repos[:cfg.BatchLimit]
	}

	prompt := ResolvePrompt(cfg.SystemPrompt, cfg.Tone)
	consecutiveServiceErrors := 0
	serviceFailures := 0
	completed := 0
	var runErrors []error

	for _, r := range repos {
		readme, err := FetchReadme(client, r.FullName, cfg.MaxReadmeChars)
		if err != nil {
			if errors.Is(err, ErrReadmeNotFound) {
				log.Printf("[summarize] %s has no README; using repository description", r.FullName)
				summary := r.Description
				if summary == "" {
					summary = r.Name
				}
				if err := db.SetSummary(database, r.ID, summary); err != nil {
					return fmt.Errorf("save fallback summary for %s: %w", r.FullName, err)
				}
				completed++
				consecutiveServiceErrors = 0
				continue
			}
			log.Printf("[summarize] transient README error for %s: %v", r.FullName, err)
			runErrors = append(runErrors, fmt.Errorf("read README for %s: %w", r.FullName, err))
			consecutiveServiceErrors++
			serviceFailures++
			if consecutiveServiceErrors >= 3 {
				return fmt.Errorf("summarization stopped after %d consecutive upstream errors: %w", consecutiveServiceErrors, errors.Join(runErrors...))
			}
			continue
		}

		summary, err := CallLLMWithRetry(client, llm, prompt, r.FullName, readme)
		if err != nil {
			log.Printf("[summarize] LLM error %s: %v, skipping (will retry next run)", r.FullName, err)
			runErrors = append(runErrors, fmt.Errorf("summarize %s: %w", r.FullName, err))
			var serviceError *llmServiceError
			if errors.As(err, &serviceError) {
				consecutiveServiceErrors++
				serviceFailures++
				if consecutiveServiceErrors >= 3 {
					return fmt.Errorf("summarization stopped after %d consecutive service errors: %w", consecutiveServiceErrors, errors.Join(runErrors...))
				}
			} else {
				consecutiveServiceErrors = 0
			}
			continue // don't save fallback — leave unsummarized so it retries next run
		}

		consecutiveServiceErrors = 0
		if err := db.SetSummary(database, r.ID, summary); err != nil {
			return fmt.Errorf("save summary for %s: %w", r.FullName, err)
		}
		completed++
		log.Printf("[summarize] ✓ %s", r.FullName)
		time.Sleep(500 * time.Millisecond)
	}
	if len(runErrors) > 0 {
		log.Printf("[summarize] completed with %d repository-level errors; failed rows remain queued", len(runErrors))
		if completed == 0 && serviceFailures > 0 {
			return fmt.Errorf("summarization failed for every attempted repository: %w", errors.Join(runErrors...))
		}
	}
	return nil
}

func ResolvePrompt(template, tone string) string {
	return strings.ReplaceAll(template, "{{.Tone}}", tone)
}

func FetchReadme(client *http.Client, fullName string, maxChars int) (string, error) {
	if maxChars <= 0 || maxChars > config.MaxReadmeCharsLimit {
		return "", fmt.Errorf("max README characters must be between 1 and %d", config.MaxReadmeCharsLimit)
	}
	maxBytes := int64(maxChars)*4 + 1
	for _, path := range readmePaths {
		resp, err := client.Get(fmt.Sprintf("https://raw.githubusercontent.com/%s/HEAD/%s", fullName, path))
		if err != nil {
			return "", fmt.Errorf("request %s: %w", path, err)
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxBytes))
		resp.Body.Close()
		if readErr != nil {
			return "", fmt.Errorf("read %s: %w", path, readErr)
		}
		if resp.StatusCode == http.StatusNotFound {
			continue
		}
		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("fetch %s returned HTTP %d", path, resp.StatusCode)
		}
		if len(body) == 0 {
			continue
		}
		runes := []rune(string(body))
		if len(runes) > maxChars {
			runes = runes[:maxChars]
		}
		return string(runes), nil
	}
	return "", ErrReadmeNotFound
}

func CallLLMWithRetry(client *http.Client, llm LLMConfig, systemPrompt, repoName, readme string) (string, error) {
	// Normalize API base URL
	base := strings.TrimRight(llm.APIBase, "/")
	if !strings.HasSuffix(base, "/v1") {
		base += "/v1"
	}
	normalized := LLMConfig{APIBase: base, APIKey: llm.APIKey, Model: llm.Model}

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			wait := time.Duration(attempt*attempt) * 5 * time.Second // 5s, 20s
			log.Printf("[summarize] retry %d for %s, waiting %v", attempt, repoName, wait)
			time.Sleep(wait)
		}
		summary, retryable, err := callLLMOnce(client, normalized, systemPrompt, repoName, readme)
		if err == nil {
			return summary, nil
		}
		lastErr = err
		if !retryable {
			return "", err
		}
	}
	return "", fmt.Errorf("after 3 attempts: %w", lastErr)
}

func callLLMOnce(client *http.Client, llm LLMConfig, systemPrompt, repoName, readme string) (summary string, retryable bool, err error) {
	body, err := json.Marshal(chatReq{
		Model: llm.Model,
		Messages: []msg{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: fmt.Sprintf("Repository: %s\n\nREADME:\n%s", repoName, readme)},
		},
	})
	if err != nil {
		return "", false, err
	}

	req, err := http.NewRequest("POST", llm.APIBase+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", false, &llmServiceError{err: fmt.Errorf("construct LLM request: %w", err)}
	}
	req.Header.Set("Content-Type", "application/json")
	if llm.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+llm.APIKey)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", true, &llmServiceError{err: err} // network error, retryable
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024+1))
	if err != nil {
		return "", true, &llmServiceError{err: fmt.Errorf("read LLM response: %w", err)}
	}
	if len(respBody) > 2*1024*1024 {
		return "", false, &llmServiceError{err: fmt.Errorf("LLM response exceeds 2 MiB")}
	}

	switch {
	case resp.StatusCode == 429:
		return "", true, &llmServiceError{err: fmt.Errorf("rate limited (429)")}
	case resp.StatusCode >= 500:
		return "", true, &llmServiceError{err: fmt.Errorf("server error (%d)", resp.StatusCode)}
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound:
		return "", false, &llmServiceError{err: fmt.Errorf("LLM API %d: %s", resp.StatusCode, string(respBody))}
	case resp.StatusCode != 200:
		return "", false, &llmServiceError{err: fmt.Errorf("LLM API %d: %s", resp.StatusCode, string(respBody))}
	}

	var cr chatResp
	if err := json.Unmarshal(respBody, &cr); err != nil {
		return "", false, &llmServiceError{err: fmt.Errorf("decode LLM response: %w", err)}
	}
	if len(cr.Choices) == 0 {
		return "", false, &llmServiceError{err: fmt.Errorf("no choices returned")}
	}

	s, parseErr := ParseLLMResponse(cr.Choices[0].Message.Content)
	return s, false, parseErr
}

func ParseLLMResponse(content string) (string, error) {
	var result SummaryResult
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		// The configured prompt normally returns plain text.
		return db.NormalizeSummary(content)
	}
	return db.NormalizeSummary(result.Summary)
}

package summarize

import (
	"bytes"
	"database/sql"
	"encoding/json"
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

func Run(database *sql.DB, llm LLMConfig, cfg *config.SummarizeConfig) error {
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
			db.SetSummary(database, r.ID, summary, "")
		}
		return nil
	}

	if cfg.BatchLimit > 0 && len(repos) > cfg.BatchLimit {
		log.Printf("[summarize] batch limit %d, processing %d of %d", cfg.BatchLimit, cfg.BatchLimit, len(repos))
		repos = repos[:cfg.BatchLimit]
	}

	prompt := ResolvePrompt(cfg.SystemPrompt, cfg.Tone)
	client := &http.Client{Timeout: 60 * time.Second}
	consecutiveErrors := 0

	for _, r := range repos {
		// Circuit breaker: stop if 3+ consecutive LLM errors (likely rate limited or down)
		if consecutiveErrors >= 3 {
			log.Printf("[summarize] stopping: %d consecutive LLM errors, likely rate limited", consecutiveErrors)
			break
		}

		readme, err := FetchReadme(client, r.FullName)
		if err != nil {
			log.Printf("[summarize] skip %s (no readme): %v", r.FullName, err)
			summary := r.Description
			if summary == "" {
				summary = r.Name
			}
			db.SetSummary(database, r.ID, summary, "")
			continue
		}

		aiInput := readme
		if len(aiInput) > cfg.MaxReadmeChars {
			aiInput = aiInput[:cfg.MaxReadmeChars]
		}

		summary, err := CallLLMWithRetry(client, llm, prompt, r.FullName, aiInput)
		if err != nil {
			log.Printf("[summarize] LLM error %s: %v, skipping (will retry next run)", r.FullName, err)
			consecutiveErrors++
			continue // don't save fallback — leave unsummarized so it retries next run
		}

		consecutiveErrors = 0
		if err := db.SetSummary(database, r.ID, summary, readme); err != nil {
			log.Printf("[summarize] db error %s: %v", r.FullName, err)
		}
		log.Printf("[summarize] ✓ %s", r.FullName)
		time.Sleep(500 * time.Millisecond)
	}
	return nil
}

func ResolvePrompt(template, tone string) string {
	return strings.ReplaceAll(template, "{{.Tone}}", tone)
}

func FetchReadme(client *http.Client, fullName string) (string, error) {
	for _, path := range readmePaths {
		resp, err := client.Get(fmt.Sprintf("https://raw.githubusercontent.com/%s/HEAD/%s", fullName, path))
		if err != nil {
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode == 200 && len(body) > 0 {
			return string(body), nil
		}
	}
	return "", fmt.Errorf("no readme found")
}

func CallLLMWithRetry(client *http.Client, llm LLMConfig, systemPrompt, repoName, readme string) (string, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			wait := time.Duration(attempt*attempt) * 5 * time.Second // 5s, 20s
			log.Printf("[summarize] retry %d for %s, waiting %v", attempt, repoName, wait)
			time.Sleep(wait)
		}
		summary, retryable, err := callLLMOnce(client, llm, systemPrompt, repoName, readme)
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
	body, _ := json.Marshal(chatReq{
		Model: llm.Model,
		Messages: []msg{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: fmt.Sprintf("Repository: %s\n\nREADME:\n%s", repoName, readme)},
		},
	})

	req, _ := http.NewRequest("POST", llm.APIBase+"/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if llm.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+llm.APIKey)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", true, err // network error, retryable
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	switch {
	case resp.StatusCode == 429:
		return "", true, fmt.Errorf("rate limited (429)")
	case resp.StatusCode == 500 || resp.StatusCode == 502 || resp.StatusCode == 503:
		return "", true, fmt.Errorf("server error (%d)", resp.StatusCode)
	case resp.StatusCode != 200:
		return "", false, fmt.Errorf("LLM API %d: %s", resp.StatusCode, string(respBody))
	}

	var cr chatResp
	if err := json.Unmarshal(respBody, &cr); err != nil {
		return "", false, err
	}
	if len(cr.Choices) == 0 {
		return "", false, fmt.Errorf("no choices returned")
	}

	s, parseErr := ParseLLMResponse(cr.Choices[0].Message.Content)
	return s, false, parseErr
}

func ParseLLMResponse(content string) (string, error) {
	var result SummaryResult
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		// LLM returned plain text, use as-is
		return content, nil
	}
	return result.Summary, nil
}

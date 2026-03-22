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
	Install string `json:"install"`
}

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
			db.SetSummary(database, r.ID, summary, "See GitHub repository for installation instructions.")
		}
		return nil
	}

	// Apply batch limit
	if cfg.BatchLimit > 0 && len(repos) > cfg.BatchLimit {
		log.Printf("[summarize] batch limit %d, processing %d of %d", cfg.BatchLimit, cfg.BatchLimit, len(repos))
		repos = repos[:cfg.BatchLimit]
	}

	// Resolve the system prompt template with tone
	prompt := ResolvePrompt(cfg.SystemPrompt, cfg.Tone)

	client := &http.Client{Timeout: 60 * time.Second}

	for _, r := range repos {
		readme, err := FetchReadme(client, r.FullName)
		if err != nil {
			log.Printf("[summarize] skip %s (no readme): %v", r.FullName, err)
			summary := r.Description
			if summary == "" {
				summary = r.Name
			}
			db.SetSummary(database, r.ID, summary, "See GitHub repository for installation instructions.")
			continue
		}

		if len(readme) > cfg.MaxReadmeChars {
			readme = readme[:cfg.MaxReadmeChars]
		}

		summary, install, err := CallLLM(client, llm, prompt, r.FullName, readme)
		if err != nil {
			log.Printf("[summarize] LLM error %s: %v, using description", r.FullName, err)
			summary = r.Description
			install = "See GitHub repository for installation instructions."
		}

		if err := db.SetSummary(database, r.ID, summary, install); err != nil {
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
	resp, err := client.Get(fmt.Sprintf("https://raw.githubusercontent.com/%s/HEAD/README.md", fullName))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("status %d", resp.StatusCode)
	}
	b, err := io.ReadAll(resp.Body)
	return string(b), err
}

func CallLLM(client *http.Client, llm LLMConfig, systemPrompt, repoName, readme string) (string, string, error) {
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
		return "", "", err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", "", fmt.Errorf("LLM API %d: %s", resp.StatusCode, string(respBody))
	}

	var cr chatResp
	if err := json.Unmarshal(respBody, &cr); err != nil {
		return "", "", err
	}
	if len(cr.Choices) == 0 {
		return "", "", fmt.Errorf("no choices returned")
	}

	return ParseLLMResponse(cr.Choices[0].Message.Content)
}

func ParseLLMResponse(content string) (summary, install string, err error) {
	var result SummaryResult
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		return content, "See GitHub repository for installation instructions.", nil
	}
	if result.Install == "" {
		result.Install = "See GitHub repository for installation instructions."
	}
	return result.Summary, result.Install, nil
}

package fetch

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/rainmana/hackyfeed/internal/config"
	"github.com/rainmana/hackyfeed/internal/db"
)

type ghSearchResult struct {
	Items []ghRepo `json:"items"`
}

type ghRepo struct {
	FullName    string    `json:"full_name"`
	Owner       ghOwner   `json:"owner"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	HTMLURL     string    `json:"html_url"`
	Stars       int       `json:"stargazers_count"`
	Language    string    `json:"language"`
	Topics      []string  `json:"topics"`
	PushedAt    time.Time `json:"pushed_at"`
	Archived    bool      `json:"archived"`
}

type ghOwner struct {
	Login string `json:"login"`
}

func Run(database *sql.DB, token string, cfg *config.FetchConfig) error {
	client := &http.Client{Timeout: 30 * time.Second}
	attempted := 0
	succeeded := 0
	var discoveryErrors []error

	for _, topic := range cfg.Topics {
		attempted++
		log.Printf("[fetch] topic: %s", topic)
		if err := fetchTopic(client, database, token, topic, cfg.MinStars); err != nil {
			log.Printf("[fetch] error on topic %s: %v", topic, err)
			discoveryErrors = append(discoveryErrors, fmt.Errorf("topic %s: %w", topic, err))
		} else {
			succeeded++
		}
		time.Sleep(2 * time.Second)
	}

	for _, awesomeURL := range cfg.AwesomeLists {
		attempted++
		log.Printf("[fetch] awesome list: %s", awesomeURL)
		if err := fetchAwesome(client, database, awesomeURL); err != nil {
			log.Printf("[fetch] error on awesome list: %v", err)
			discoveryErrors = append(discoveryErrors, fmt.Errorf("awesome list %s: %w", awesomeURL, err))
		} else {
			succeeded++
		}
	}
	if attempted == 0 {
		return fmt.Errorf("no discovery sources configured")
	}
	if succeeded == 0 {
		return fmt.Errorf("all %d discovery sources failed: %w", attempted, errors.Join(discoveryErrors...))
	}
	if len(discoveryErrors) > 0 {
		log.Printf("[fetch] completed with %d of %d sources successful", succeeded, attempted)
	}
	return nil
}

func fetchTopic(client *http.Client, database *sql.DB, token, topic string, minStars int) error {
	for page := 1; page <= 3; page++ {
		u := githubSearchURL(topic, minStars, page)

		req, err := http.NewRequest("GET", u, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}

		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return readErr
		}

		if resp.StatusCode != 200 {
			detail := strings.TrimSpace(string(body))
			if len(detail) > 500 {
				detail = detail[:500]
			}
			return fmt.Errorf("GitHub API %d for page %d: %s", resp.StatusCode, page, detail)
		}

		var result ghSearchResult
		if err := json.Unmarshal(body, &result); err != nil {
			return err
		}

		for _, r := range result.Items {
			if r.Archived || !IsLikelyEnglish(r.Description) {
				continue
			}
			if err := db.UpsertRepo(database, &db.Repo{
				FullName:    r.FullName,
				Owner:       r.Owner.Login,
				Name:        r.Name,
				Description: r.Description,
				HTMLURL:     r.HTMLURL,
				Stars:       r.Stars,
				Language:    r.Language,
				Topics:      strings.Join(r.Topics, ","),
				LastPushed:  r.PushedAt,
				Source:      "github-topic",
			}); err != nil {
				return fmt.Errorf("upsert %s: %w", r.FullName, err)
			}
		}

		if len(result.Items) < 100 {
			break
		}
		time.Sleep(2 * time.Second)
	}
	return nil
}

func githubSearchURL(topic string, minStars, page int) string {
	q := fmt.Sprintf("topic:%s stars:>=%d archived:false", topic, minStars)
	// Recent activity is the discovery signal. Sorting by stars permanently hid
	// new qualifying projects below the historical top 300 for busy topics.
	return fmt.Sprintf("https://api.github.com/search/repositories?q=%s&sort=updated&order=desc&per_page=100&page=%d", url.QueryEscape(q), page)
}

var ReGHLink = regexp.MustCompile(`(?i)\[[^\]]+\]\((https://github\.com/[^\s)]+)(?:\s+(?:"[^"]*"|'[^']*'))?\)`)

// IsLikelyEnglish returns true if the text is empty or mostly ASCII/Latin characters.
func IsLikelyEnglish(text string) bool {
	if text == "" {
		return true
	}
	ascii := 0
	for _, r := range text {
		if r < 128 {
			ascii++
		}
	}
	return float64(ascii)/float64(len([]rune(text))) > 0.7
}

func ParseAwesomeMarkdown(text string) []db.Repo {
	var repos []db.Repo
	for _, line := range strings.Split(text, "\n") {
		for _, m := range ReGHLink.FindAllStringSubmatch(line, -1) {
			parsed, err := url.Parse(m[1])
			if err != nil || parsed.User != nil || parsed.Port() != "" || parsed.Scheme != "https" || !strings.EqualFold(parsed.Hostname(), "github.com") {
				continue
			}
			parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
			if len(parts) != 2 {
				continue
			}
			fullName := parts[0] + "/" + parts[1]
			owner, name, htmlURL, err := db.CanonicalGitHubIdentity(fullName)
			if err != nil {
				continue
			}
			desc := ""
			if idx := strings.Index(line, " - "); idx != -1 {
				desc = strings.TrimSpace(line[idx+3:])
			}
			repos = append(repos, db.Repo{
				FullName:    fullName,
				Owner:       owner,
				Name:        name,
				Description: desc,
				HTMLURL:     htmlURL,
				Source:      "awesome-list",
			})
		}
	}
	return repos
}

func fetchAwesome(client *http.Client, database *sql.DB, rawURL string) error {
	const maxAwesomeBytes = 10 * 1024 * 1024
	resp, err := client.Get(rawURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxAwesomeBytes+1))
	if err != nil {
		return err
	}
	if len(body) > maxAwesomeBytes {
		return fmt.Errorf("awesome list exceeds %d bytes", maxAwesomeBytes)
	}

	repos := ParseAwesomeMarkdown(string(body))
	if len(repos) == 0 {
		return fmt.Errorf("awesome list contained no valid GitHub repository links")
	}
	for _, r := range repos {
		if err := db.UpsertRepo(database, &r); err != nil {
			return fmt.Errorf("upsert %s: %w", r.FullName, err)
		}
	}
	log.Printf("[fetch] parsed %d repos from awesome list", len(repos))
	return nil
}

package fetch

import (
	"bufio"
	"database/sql"
	"encoding/json"
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

	for _, topic := range cfg.Topics {
		log.Printf("[fetch] topic: %s", topic)
		if err := fetchTopic(client, database, token, topic, cfg.MinStars); err != nil {
			log.Printf("[fetch] error on topic %s: %v", topic, err)
		}
		time.Sleep(2 * time.Second)
	}

	for _, awesomeURL := range cfg.AwesomeLists {
		log.Printf("[fetch] awesome list: %s", awesomeURL)
		if err := fetchAwesome(client, database, awesomeURL); err != nil {
			log.Printf("[fetch] error on awesome list: %v", err)
		}
	}
	return nil
}

func fetchTopic(client *http.Client, database *sql.DB, token, topic string, minStars int) error {
	for page := 1; page <= 3; page++ {
		q := fmt.Sprintf("topic:%s stars:>=%d archived:false", topic, minStars)
		u := fmt.Sprintf("https://api.github.com/search/repositories?q=%s&sort=stars&order=desc&per_page=100&page=%d", url.QueryEscape(q), page)

		req, _ := http.NewRequest("GET", u, nil)
		req.Header.Set("Accept", "application/vnd.github+json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}

		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != 200 {
			log.Printf("[fetch] GitHub API %d for topic %s page %d", resp.StatusCode, topic, page)
			break
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
				log.Printf("[fetch] upsert error %s: %v", r.FullName, err)
			}
		}

		if len(result.Items) < 100 {
			break
		}
		time.Sleep(2 * time.Second)
	}
	return nil
}

var ReGHLink = regexp.MustCompile(`\[([^\]]+)\]\(https://github\.com/([^/]+/[^/)]+)\)`)

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
	scanner := bufio.NewScanner(strings.NewReader(text))
	for scanner.Scan() {
		line := scanner.Text()
		for _, m := range ReGHLink.FindAllStringSubmatch(line, -1) {
			fullName := m[2]
			parts := strings.SplitN(fullName, "/", 2)
			if len(parts) != 2 {
				continue
			}
			desc := ""
			if idx := strings.Index(line, " - "); idx != -1 {
				desc = strings.TrimSpace(line[idx+3:])
			}
			repos = append(repos, db.Repo{
				FullName:    fullName,
				Owner:       parts[0],
				Name:        parts[1],
				Description: desc,
				HTMLURL:     "https://github.com/" + fullName,
				Source:      "awesome-list",
			})
		}
	}
	return repos
}

func fetchAwesome(client *http.Client, database *sql.DB, rawURL string) error {
	resp, err := client.Get(rawURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	repos := ParseAwesomeMarkdown(string(body))
	for _, r := range repos {
		if err := db.UpsertRepo(database, &r); err != nil {
			log.Printf("[fetch] awesome upsert error %s: %v", r.FullName, err)
		}
	}
	log.Printf("[fetch] parsed %d repos from awesome list", len(repos))
	return nil
}

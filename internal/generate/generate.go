package generate

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/rainmana/hackyfeed/internal/config"
	"github.com/rainmana/hackyfeed/internal/db"
)

func Run(database *sql.DB, siteDir string, cfg *config.CategoriesConfig) error {
	repos, err := db.Unpublished(database)
	if err != nil {
		return err
	}
	log.Printf("[generate] %d new repos to publish", len(repos))

	toolsDir := filepath.Join(siteDir, "content", "tools")
	os.MkdirAll(toolsDir, 0755)

	for _, r := range repos {
		if err := writeToolPage(toolsDir, r, cfg); err != nil {
			log.Printf("[generate] write error %s: %v", r.FullName, err)
			continue
		}
		if err := db.MarkPublished(database, r.ID); err != nil {
			log.Printf("[generate] mark error %s: %v", r.FullName, err)
		}
		log.Printf("[generate] ✓ %s", r.FullName)
	}

	return regenerateAll(database, siteDir, cfg)
}

func regenerateAll(database *sql.DB, siteDir string, cfg *config.CategoriesConfig) error {
	repos, err := db.AllPublished(database)
	if err != nil {
		return err
	}
	toolsDir := filepath.Join(siteDir, "content", "tools")
	for _, r := range repos {
		writeToolPage(toolsDir, r, cfg)
	}
	log.Printf("[generate] regenerated %d total tool pages", len(repos))
	return nil
}

func writeToolPage(toolsDir string, r db.Repo, cfg *config.CategoriesConfig) error {
	slug := Slugify(r.FullName)
	path := filepath.Join(toolsDir, slug+".md")
	content := RenderToolMarkdown(r, cfg)
	return os.WriteFile(path, []byte(content), 0644)
}

func Slugify(fullName string) string {
	return strings.ReplaceAll(strings.ToLower(fullName), "/", "-")
}

func RenderToolMarkdown(r db.Repo, cfg *config.CategoriesConfig) string {
	categories := CategorizeTool(r, cfg)
	catYAML := "  - " + strings.Join(categories, "\n  - ")

	tagYAML := ""
	if r.Language != "" {
		tagYAML = fmt.Sprintf("tags:\n  - %s", strings.ToLower(r.Language))
	}

	// Build the body: AI summary at top, then full README
	var body strings.Builder
	if r.AISummary != "" {
		body.WriteString("> **AI Summary:** ")
		body.WriteString(r.AISummary)
		body.WriteString("\n\n---\n\n")
	}
	if r.ReadmeRaw != "" {
		body.WriteString(r.ReadmeRaw)
	} else {
		body.WriteString(r.Description)
	}

	return fmt.Sprintf(`---
title: "%s"
date: %s
categories:
%s
%s
github_url: "%s"
stars: %d
language: "%s"
source: "%s"
---

%s
`, escapeYAML(r.Name), r.FirstSeen.Format("2006-01-02"),
		catYAML, tagYAML,
		r.HTMLURL, r.Stars, r.Language, r.Source,
		body.String())
}

func escapeYAML(s string) string {
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}

func CategorizeTool(r db.Repo, cfg *config.CategoriesConfig) []string {
	text := strings.ToLower(r.Topics + " " + r.Description + " " + r.Name)
	cats := map[string]bool{}

	if r.Source == "awesome-list" || r.Source == "awesome-rainmana" {
		cats["awesome-list"] = true
	}

	for cat, keywords := range cfg.Rules {
		for _, kw := range keywords {
			if strings.Contains(text, kw) {
				cats[cat] = true
				break
			}
		}
	}

	if len(cats) == 0 {
		cats[cfg.DefaultCategory] = true
	}

	result := make([]string, 0, len(cats))
	for c := range cats {
		result = append(result, c)
	}
	return result
}

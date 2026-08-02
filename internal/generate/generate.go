package generate

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/BurntSushi/toml"
	"github.com/rainmana/hackyfeed/internal/config"
	"github.com/rainmana/hackyfeed/internal/db"
)

func Run(database *sql.DB, siteDir string, cfg *config.Config) error {
	return runWithPageWriter(database, siteDir, cfg, os.WriteFile)
}

func runWithPageWriter(database *sql.DB, siteDir string, cfg *config.Config, writeFile func(string, []byte, os.FileMode) error) error {
	repos, err := db.AllSummarized(database)
	if err != nil {
		return err
	}
	log.Printf("[generate] rendering %d tool pages", len(repos))

	if err := os.MkdirAll(siteDir, 0755); err != nil {
		return fmt.Errorf("create site directory: %w", err)
	}
	if err := writeHugoConfig(siteDir, &cfg.Site); err != nil {
		return fmt.Errorf("write generated Hugo config: %w", err)
	}
	toolsDir := filepath.Join(siteDir, "content", "tools")
	contentDir := filepath.Dir(toolsDir)
	if err := os.MkdirAll(contentDir, 0755); err != nil {
		return fmt.Errorf("create content directory: %w", err)
	}
	stagedToolsDir, err := os.MkdirTemp(contentDir, ".tools-stage-*")
	if err != nil {
		return fmt.Errorf("stage tools directory: %w", err)
	}
	defer os.RemoveAll(stagedToolsDir)
	toolSlugs := resolveToolSlugs(repos)
	for index, r := range repos {
		if err := writeToolPage(stagedToolsDir, r, &cfg.Categories, toolSlugs[r.FullName], writeFile); err != nil {
			return fmt.Errorf("write %s: %w", r.FullName, err)
		}
		if (index+1)%100 == 0 {
			log.Printf("[generate] staged %d/%d tool pages", index+1, len(repos))
		}
	}
	if err := replaceGeneratedDirectory(stagedToolsDir, toolsDir); err != nil {
		return fmt.Errorf("publish generated tools: %w", err)
	}

	log.Printf("[generate] rendered %d total tool pages", len(repos))
	return nil
}

func resolveToolSlugs(repos []db.Repo) map[string]string {
	groups := make(map[string][]db.Repo, len(repos))
	for _, repo := range repos {
		legacy := Slugify(repo.FullName)
		groups[legacy] = append(groups[legacy], repo)
	}

	resolved := make(map[string]string, len(repos))
	used := make(map[string]bool, len(repos))
	var collisions []db.Repo
	for legacy, group := range groups {
		sort.Slice(group, func(i, j int) bool {
			if group[i].FirstSeen.Equal(group[j].FirstSeen) {
				return group[i].FullName < group[j].FullName
			}
			return group[i].FirstSeen.Before(group[j].FirstSeen)
		})
		resolved[group[0].FullName] = legacy
		used[legacy] = true
		collisions = append(collisions, group[1:]...)
	}
	sort.Slice(collisions, func(i, j int) bool { return collisions[i].FullName < collisions[j].FullName })
	for _, repo := range collisions {
		base := collisionSlug(repo.FullName)
		candidate := base
		for attempt := 0; used[candidate]; attempt++ {
			digest := sha256.Sum256([]byte(repo.FullName + "\x00" + strconv.Itoa(attempt)))
			candidate = base + "-" + hex.EncodeToString(digest[:8])
		}
		resolved[repo.FullName] = candidate
		used[candidate] = true
	}
	return resolved
}

func replaceGeneratedDirectory(stagedDir, targetDir string) error {
	parent := filepath.Dir(targetDir)
	backupDir := ""
	if info, err := os.Stat(targetDir); err == nil {
		if !info.IsDir() {
			return fmt.Errorf("target %s is not a directory", targetDir)
		}
		placeholder, err := os.MkdirTemp(parent, ".tools-backup-*")
		if err != nil {
			return err
		}
		backupDir = placeholder
		if err := os.Remove(backupDir); err != nil {
			return err
		}
		if err := os.Rename(targetDir, backupDir); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	if err := os.Rename(stagedDir, targetDir); err != nil {
		if backupDir != "" {
			if rollbackErr := os.Rename(backupDir, targetDir); rollbackErr != nil {
				return fmt.Errorf("activate staged directory: %v; rollback failed: %w", err, rollbackErr)
			}
		}
		return err
	}
	if backupDir != "" {
		if err := os.RemoveAll(backupDir); err != nil {
			return fmt.Errorf("remove generated backup %s: %w", backupDir, err)
		}
	}
	return nil
}

type generatedHugoConfig struct {
	BaseURL string              `toml:"baseURL"`
	Title   string              `toml:"title"`
	Params  generatedHugoParams `toml:"params"`
}

type generatedHugoParams struct {
	Description string `toml:"description"`
	Author      string `toml:"author"`
	Tagline     string `toml:"tagline"`
}

func writeHugoConfig(siteDir string, site *config.SiteConfig) error {
	contents, err := renderHugoConfig(site)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(siteDir, "hackyfeed.generated.toml"), contents, 0644)
}

func renderHugoConfig(site *config.SiteConfig) ([]byte, error) {
	if strings.TrimSpace(site.Title) == "" {
		return nil, fmt.Errorf("site title is empty")
	}
	parsedBaseURL, err := url.Parse(site.BaseURL)
	if err != nil || (parsedBaseURL.Scheme != "http" && parsedBaseURL.Scheme != "https") || parsedBaseURL.Host == "" {
		return nil, fmt.Errorf("invalid site base_url %q", site.BaseURL)
	}

	generated := generatedHugoConfig{
		BaseURL: site.BaseURL,
		Title:   site.Title,
		Params: generatedHugoParams{
			Description: site.Description,
			Author:      site.Author,
			Tagline:     site.Tagline,
		},
	}
	var output bytes.Buffer
	if err := toml.NewEncoder(&output).Encode(generated); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func writeToolPage(toolsDir string, r db.Repo, cfg *config.CategoriesConfig, slug string, writeFile func(string, []byte, os.FileMode) error) error {
	path := filepath.Join(toolsDir, contentFileName(r.FullName))
	content := renderToolMarkdown(r, cfg, slug)
	return writeFile(path, []byte(content), 0644)
}

func contentFileName(fullName string) string {
	digest := sha256.Sum256([]byte(fullName))
	return hex.EncodeToString(digest[:16]) + ".md"
}

func Slugify(fullName string) string {
	return strings.ReplaceAll(strings.ToLower(fullName), "/", "-")
}

func collisionSlug(fullName string) string {
	normalized := strings.ToLower(fullName)
	parts := strings.SplitN(normalized, "/", 2)
	if len(parts) != 2 {
		return fmt.Sprintf("%d-%s", len(normalized), strings.ReplaceAll(normalized, "/", "-"))
	}
	return fmt.Sprintf("%d-%s-%s", len(parts[0]), parts[0], parts[1])
}

func RenderToolMarkdown(r db.Repo, cfg *config.CategoriesConfig) string {
	return renderToolMarkdown(r, cfg, Slugify(r.FullName))
}

func renderToolMarkdown(r db.Repo, cfg *config.CategoriesConfig, slug string) string {
	categories := CategorizeTool(r, cfg)
	catYAML := yamlList(categories)

	tagYAML := ""
	if r.Language != "" {
		tagYAML = fmt.Sprintf("tags:\n  - %s\n", yamlString(strings.ToLower(r.Language)))
	}

	summary := normalizeSummary(r.AISummary)
	if summary == "" {
		summary = normalizeSummary(r.Description)
	}
	if summary == "" {
		summary = r.Name
	}

	publishedAt := r.FirstSeen
	if publishedAt.IsZero() {
		publishedAt = time.Unix(0, 0).UTC()
	}

	return fmt.Sprintf(`---
title: %s
slug: %s
date: %s
summary: %s
description: %s
categories:
%s
%sgithub_url: %s
stars: %d
language: %s
source: %s
---
`, yamlString(r.Name), yamlString(slug), publishedAt.UTC().Format(time.RFC3339),
		yamlString(summary), yamlString(summary), catYAML, tagYAML,
		yamlString(r.HTMLURL), r.Stars, yamlString(r.Language), yamlString(r.Source))
}

func yamlList(items []string) string {
	lines := make([]string, 0, len(items))
	for _, item := range items {
		lines = append(lines, "  - "+yamlString(item))
	}
	return strings.Join(lines, "\n")
}

func yamlString(s string) string {
	var output strings.Builder
	output.Grow(2 + len(s)*6)
	output.WriteByte('"')
	for _, character := range s {
		if character <= 0xffff {
			fmt.Fprintf(&output, `\u%04X`, character)
		} else {
			fmt.Fprintf(&output, `\U%08X`, character)
		}
	}
	output.WriteByte('"')
	return output.String()
}

func normalizeSummary(s string) string {
	normalized, err := db.NormalizeSummary(s)
	if err != nil {
		return ""
	}
	return normalized
}

func CategorizeTool(r db.Repo, cfg *config.CategoriesConfig) []string {
	text := strings.ToLower(r.Topics + " " + r.Description + " " + r.Name)
	cats := map[string]bool{}

	if r.Source == "awesome-list" || r.Source == "awesome-rainmana" {
		cats["awesome-list"] = true
	}

	for cat, keywords := range cfg.Rules {
		for _, kw := range keywords {
			if keywordMatches(text, strings.ToLower(strings.TrimSpace(kw))) {
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
	sort.Strings(result)
	return result
}

func keywordMatches(text, keyword string) bool {
	if keyword == "" {
		return false
	}
	if len([]rune(keyword)) <= 4 {
		alphaNumeric := true
		for _, character := range keyword {
			if !unicode.IsLetter(character) && !unicode.IsDigit(character) {
				alphaNumeric = false
				break
			}
		}
		if alphaNumeric {
			for _, token := range strings.FieldsFunc(text, func(character rune) bool {
				return !unicode.IsLetter(character) && !unicode.IsDigit(character)
			}) {
				if token == keyword {
					return true
				}
			}
			return false
		}
	}
	return strings.Contains(text, keyword)
}

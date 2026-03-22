package config

import (
	"os"
	"testing"
)

func TestLoadDefault(t *testing.T) {
	// Loading a nonexistent file should return defaults
	cfg, err := Load("/nonexistent/hackyfeed.toml")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Site.Title != "HackyFeed" {
		t.Errorf("expected default title, got %q", cfg.Site.Title)
	}
	if len(cfg.Fetch.Topics) == 0 {
		t.Error("expected default topics")
	}
	if cfg.Fetch.MinStars != 10 {
		t.Errorf("expected 10 min stars, got %d", cfg.Fetch.MinStars)
	}
	if cfg.Categories.DefaultCategory != "security-tools" {
		t.Errorf("expected default category, got %q", cfg.Categories.DefaultCategory)
	}
}

func TestLoadCustomConfig(t *testing.T) {
	content := `
[site]
title = "MyFeed"
author = "tester"

[fetch]
topics = ["ai", "machine-learning"]
min_stars = 50

[categories]
default_category = "ai-tools"
[categories.rules]
ai = ["artificial-intelligence", "machine-learning"]
`
	f, _ := os.CreateTemp("", "hackyfeed-test-*.toml")
	f.WriteString(content)
	f.Close()
	defer os.Remove(f.Name())

	cfg, err := Load(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Site.Title != "MyFeed" {
		t.Errorf("expected MyFeed, got %q", cfg.Site.Title)
	}
	if len(cfg.Fetch.Topics) != 2 || cfg.Fetch.Topics[0] != "ai" {
		t.Errorf("expected custom topics, got %v", cfg.Fetch.Topics)
	}
	if cfg.Fetch.MinStars != 50 {
		t.Errorf("expected 50 min stars, got %d", cfg.Fetch.MinStars)
	}
	if cfg.Categories.DefaultCategory != "ai-tools" {
		t.Errorf("expected ai-tools, got %q", cfg.Categories.DefaultCategory)
	}
	if _, ok := cfg.Categories.Rules["ai"]; !ok {
		t.Error("expected ai category rule")
	}
}

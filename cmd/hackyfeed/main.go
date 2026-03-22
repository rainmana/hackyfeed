package main

import (
	"fmt"
	"log"
	"os"

	"github.com/rainmana/hackyfeed/internal/config"
	"github.com/rainmana/hackyfeed/internal/db"
	"github.com/rainmana/hackyfeed/internal/fetch"
	"github.com/rainmana/hackyfeed/internal/generate"
	"github.com/rainmana/hackyfeed/internal/summarize"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: hackyfeed <command>")
		fmt.Println("Commands: fetch, summarize, generate, all")
		os.Exit(1)
	}

	cfgPath := envOr("HACKYFEED_CONFIG", "hackyfeed.toml")
	cfg, err := config.Load(cfgPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	dbPath := envOr("HACKYFEED_DB", "hackyfeed.db")
	siteDir := envOr("HACKYFEED_SITE", "site")

	database, err := db.Open(dbPath)
	if err != nil {
		log.Fatalf("db open: %v", err)
	}
	defer database.Close()

	switch os.Args[1] {
	case "fetch":
		token := os.Getenv("GITHUB_TOKEN")
		if token == "" {
			log.Println("[warn] GITHUB_TOKEN not set, API rate limits will be low")
		}
		if err := fetch.Run(database, token, &cfg.Fetch); err != nil {
			log.Fatalf("fetch: %v", err)
		}

	case "summarize":
		llm := summarize.LLMConfig{
			APIBase: envOr("LLM_API_BASE", "http://localhost:4000/v1"),
			APIKey:  os.Getenv("LLM_API_KEY"),
			Model:   envOr("LLM_MODEL", "gpt-4o-mini"),
		}
		if err := summarize.Run(database, llm, &cfg.Summarize); err != nil {
			log.Fatalf("summarize: %v", err)
		}

	case "generate":
		if err := generate.Run(database, siteDir, &cfg.Categories); err != nil {
			log.Fatalf("generate: %v", err)
		}

	case "all":
		token := os.Getenv("GITHUB_TOKEN")
		log.Println("=== fetch ===")
		if err := fetch.Run(database, token, &cfg.Fetch); err != nil {
			log.Fatalf("fetch: %v", err)
		}
		log.Println("=== summarize ===")
		llm := summarize.LLMConfig{
			APIBase: envOr("LLM_API_BASE", "http://localhost:4000/v1"),
			APIKey:  os.Getenv("LLM_API_KEY"),
			Model:   envOr("LLM_MODEL", "gpt-4o-mini"),
		}
		if err := summarize.Run(database, llm, &cfg.Summarize); err != nil {
			log.Fatalf("summarize: %v", err)
		}
		log.Println("=== generate ===")
		if err := generate.Run(database, siteDir, &cfg.Categories); err != nil {
			log.Fatalf("generate: %v", err)
		}
		log.Println("=== done ===")

	default:
		fmt.Printf("Unknown command: %s\n", os.Args[1])
		os.Exit(1)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

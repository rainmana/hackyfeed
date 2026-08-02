package config

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
)

const MaxReadmeCharsLimit = 250_000

type Config struct {
	Site       SiteConfig       `toml:"site"`
	Fetch      FetchConfig      `toml:"fetch"`
	Summarize  SummarizeConfig  `toml:"summarize"`
	Categories CategoriesConfig `toml:"categories"`
}

type SiteConfig struct {
	Title       string `toml:"title"`
	BaseURL     string `toml:"base_url"`
	Description string `toml:"description"`
	Tagline     string `toml:"tagline"`
	Author      string `toml:"author"`
}

type FetchConfig struct {
	Topics       []string `toml:"topics"`
	MinStars     int      `toml:"min_stars"`
	AwesomeLists []string `toml:"awesome_lists"`
}

type SummarizeConfig struct {
	Enabled        bool   `toml:"enabled"`
	BatchLimit     int    `toml:"batch_limit"`
	Tone           string `toml:"tone"`
	SystemPrompt   string `toml:"system_prompt"`
	MaxReadmeChars int    `toml:"max_readme_chars"`
}

type CategoriesConfig struct {
	Rules           map[string][]string `toml:"rules"`
	DefaultCategory string              `toml:"default_category"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	cfg := Default()
	metadata, err := toml.Decode(string(data), cfg)
	if err != nil {
		return nil, err
	}
	if undecoded := metadata.Undecoded(); len(undecoded) > 0 {
		return nil, fmt.Errorf("unknown configuration keys: %v", undecoded)
	}
	if metadata.IsDefined("categories", "rules") {
		var explicit struct {
			Categories struct {
				Rules map[string][]string `toml:"rules"`
			} `toml:"categories"`
		}
		if _, err := toml.Decode(string(data), &explicit); err != nil {
			return nil, err
		}
		cfg.Categories.Rules = explicit.Categories.Rules
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (cfg *Config) Validate() error {
	if strings.TrimSpace(cfg.Site.Title) == "" {
		return fmt.Errorf("site.title must not be empty")
	}
	if strings.TrimSpace(cfg.Site.Author) == "" {
		return fmt.Errorf("site.author must not be empty")
	}
	baseURL, err := url.Parse(cfg.Site.BaseURL)
	if err != nil || (baseURL.Scheme != "http" && baseURL.Scheme != "https") || baseURL.Host == "" || baseURL.User != nil || baseURL.RawQuery != "" || baseURL.Fragment != "" {
		return fmt.Errorf("site.base_url must be an absolute HTTP(S) URL without credentials, query, or fragment")
	}
	if cfg.Fetch.MinStars < 0 {
		return fmt.Errorf("fetch.min_stars must be zero or greater")
	}
	if cfg.Summarize.BatchLimit < 0 {
		return fmt.Errorf("summarize.batch_limit must be zero or greater")
	}
	if cfg.Summarize.MaxReadmeChars <= 0 || cfg.Summarize.MaxReadmeChars > MaxReadmeCharsLimit {
		return fmt.Errorf("summarize.max_readme_chars must be between 1 and %d", MaxReadmeCharsLimit)
	}
	if strings.TrimSpace(cfg.Categories.DefaultCategory) == "" {
		return fmt.Errorf("categories.default_category must not be empty")
	}
	if len(cfg.Categories.Rules) == 0 {
		return fmt.Errorf("categories.rules must contain at least one rule")
	}
	for category, keywords := range cfg.Categories.Rules {
		if strings.TrimSpace(category) == "" || len(keywords) == 0 {
			return fmt.Errorf("categories.rules entries require a name and at least one keyword")
		}
	}
	return nil
}

func Default() *Config {
	return &Config{
		Site: SiteConfig{
			Title:       "HackyFeed",
			BaseURL:     "https://example.github.io/hackyfeed/",
			Description: "A cybersecurity tools aggregator",
			Author:      "hackyfeed",
		},
		Fetch: FetchConfig{
			Topics:   []string{"pentesting", "pentest-tool", "red-team", "exploit", "offensive-security", "hacking-tool", "osint", "vulnerability-scanner", "bug-bounty", "ctf-tools", "reverse-engineering", "malware-analysis", "security-tools", "post-exploitation", "privilege-escalation"},
			MinStars: 10,
		},
		Summarize: SummarizeConfig{
			Enabled:        true,
			BatchLimit:     0,
			Tone:           "technical",
			SystemPrompt:   "You are a cybersecurity tools cataloger. Given a GitHub repo's README content, write a concise 2-3 sentence summary of what the tool does, its primary use case, and notable features. Write in a {{.Tone}} tone. Respond with ONLY the summary text, no JSON, no markdown fences.",
			MaxReadmeChars: 4000,
		},
		Categories: CategoriesConfig{
			Rules: map[string][]string{
				"exploit":              {"exploit", "cve", "vulnerability", "0day"},
				"red-team":             {"red-team", "redteam", "c2", "command-and-control"},
				"pentesting":           {"pentest", "pentesting", "penetration"},
				"osint":                {"osint", "recon", "reconnaissance", "intelligence"},
				"scanner":              {"scanner", "scan", "nmap", "port-scan"},
				"reverse-engineering":  {"reverse-engineering", "disassembl", "decompil", "binary-analysis"},
				"malware":              {"malware", "ransomware", "trojan", "rat"},
				"web-security":         {"web-security", "xss", "sqli", "injection", "burp"},
				"network":              {"network", "wireless", "wifi", "packet", "mitm"},
				"privilege-escalation": {"privilege-escalation", "privesc", "escalat"},
				"post-exploitation":    {"post-exploitation", "lateral-movement", "persistence"},
				"cryptography":         {"crypto", "encrypt", "decrypt", "cipher"},
				"forensics":            {"forensic", "dfir", "incident-response"},
				"cloud-security":       {"cloud-security", "aws-security", "azure-security"},
			},
			DefaultCategory: "security-tools",
		},
	}
}

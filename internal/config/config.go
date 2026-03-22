package config

import (
	"os"

	"github.com/BurntSushi/toml"
)

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
		return Default(), nil // fall back to defaults if no config file
	}
	cfg := Default()
	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
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
			SystemPrompt:   "You are a cybersecurity tools cataloger. Given a GitHub repo's README content, produce a JSON object with:\n- \"summary\": A concise 2-3 sentence description. Write in a {{.Tone}} tone.\n- \"install\": Brief installation instructions.\nRespond ONLY with valid JSON, no markdown fences.",
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

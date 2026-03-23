package fetch

import (
	"testing"
)

func TestParseAwesomeMarkdown(t *testing.T) {
	md := `## Go
- [owner/tool1](https://github.com/owner/tool1) - A pentesting tool for networks
- [org/scanner](https://github.com/org/scanner) - Fast port scanner
- This line has no link
## Python
- [dev/exploit-kit](https://github.com/dev/exploit-kit) - Exploit framework
`
	repos := ParseAwesomeMarkdown(md)
	if len(repos) != 3 {
		t.Fatalf("expected 3 repos, got %d", len(repos))
	}

	if repos[0].FullName != "owner/tool1" {
		t.Errorf("expected owner/tool1, got %s", repos[0].FullName)
	}
	if repos[0].Description != "A pentesting tool for networks" {
		t.Errorf("expected description, got %q", repos[0].Description)
	}
	if repos[0].Owner != "owner" {
		t.Errorf("expected owner, got %s", repos[0].Owner)
	}
	if repos[0].Source != "awesome-list" {
		t.Errorf("expected awesome-list source, got %s", repos[0].Source)
	}

	if repos[1].FullName != "org/scanner" {
		t.Errorf("expected org/scanner, got %s", repos[1].FullName)
	}

	if repos[2].FullName != "dev/exploit-kit" {
		t.Errorf("expected dev/exploit-kit, got %s", repos[2].FullName)
	}
}

func TestParseAwesomeMarkdownEdgeCases(t *testing.T) {
	// Multiple links on one line
	md := `- [a/b](https://github.com/a/b) and [c/d](https://github.com/c/d) - two tools`
	repos := ParseAwesomeMarkdown(md)
	if len(repos) != 2 {
		t.Fatalf("expected 2 repos from multi-link line, got %d", len(repos))
	}

	// No links
	md = `Just some text with no GitHub links`
	repos = ParseAwesomeMarkdown(md)
	if len(repos) != 0 {
		t.Fatalf("expected 0 repos, got %d", len(repos))
	}

	// Non-GitHub link
	md = `- [tool](https://gitlab.com/owner/tool) - Not GitHub`
	repos = ParseAwesomeMarkdown(md)
	if len(repos) != 0 {
		t.Fatalf("expected 0 repos for non-GitHub link, got %d", len(repos))
	}
}

func TestReGHLinkRegex(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{`[name](https://github.com/owner/repo)`, 1},
		{`[name](https://github.com/owner/repo) - desc`, 1},
		{`[a](https://github.com/a/b) [c](https://github.com/c/d)`, 2},
		{`no links here`, 0},
		{`[name](https://gitlab.com/owner/repo)`, 0},
	}

	for _, tt := range tests {
		matches := ReGHLink.FindAllStringSubmatch(tt.input, -1)
		if len(matches) != tt.expected {
			t.Errorf("input %q: expected %d matches, got %d", tt.input, tt.expected, len(matches))
		}
	}
}

func TestIsLikelyEnglish(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"A pentesting tool for networks", true},
		{"", true},
		{"工具描述在这里", false},
		{"Инструмент для тестирования", false},
		{"Tool with some émojis 🔥", true},
		{"Mixed 中文 and English text here for testing", true}, // >70% ASCII
	}
	for _, tt := range tests {
		got := IsLikelyEnglish(tt.input)
		if got != tt.expected {
			t.Errorf("IsLikelyEnglish(%q) = %v, want %v", tt.input, got, tt.expected)
		}
	}
}

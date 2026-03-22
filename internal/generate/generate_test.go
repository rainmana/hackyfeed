package generate

import (
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/rainmana/hackyfeed/internal/config"
	"github.com/rainmana/hackyfeed/internal/db"
)

func testCategoriesConfig() *config.CategoriesConfig {
	return &config.CategoriesConfig{
		Rules: map[string][]string{
			"exploit":    {"exploit", "cve", "vulnerability"},
			"pentesting": {"pentest", "pentesting"},
			"osint":      {"osint", "recon"},
			"scanner":    {"scanner", "scan"},
			"red-team":   {"red-team", "redteam", "c2"},
		},
		DefaultCategory: "security-tools",
	}
}

func TestCategorizeTool(t *testing.T) {
	cfg := testCategoriesConfig()

	tests := []struct {
		name     string
		repo     db.Repo
		expected []string
	}{
		{
			name:     "exploit topic",
			repo:     db.Repo{Topics: "exploit,python", Description: "An exploit framework"},
			expected: []string{"exploit"},
		},
		{
			name:     "multiple categories",
			repo:     db.Repo{Topics: "pentesting,scanner", Description: "A pentest scanner"},
			expected: []string{"pentesting", "scanner"},
		},
		{
			name:     "osint from description",
			repo:     db.Repo{Topics: "", Description: "OSINT reconnaissance tool"},
			expected: []string{"osint"},
		},
		{
			name:     "default when no match",
			repo:     db.Repo{Topics: "golang", Description: "A utility library"},
			expected: []string{"security-tools"},
		},
		{
			name:     "awesome list source",
			repo:     db.Repo{Topics: "", Description: "some tool", Source: "awesome-list"},
			expected: []string{"awesome-list"},
		},
		{
			name:     "red team from name",
			repo:     db.Repo{Name: "redteam-toolkit", Topics: "", Description: "toolkit"},
			expected: []string{"red-team"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CategorizeTool(tt.repo, cfg)
			sort.Strings(got)
			sort.Strings(tt.expected)
			if strings.Join(got, ",") != strings.Join(tt.expected, ",") {
				t.Errorf("expected %v, got %v", tt.expected, got)
			}
		})
	}
}

func TestSlugify(t *testing.T) {
	tests := []struct {
		input, expected string
	}{
		{"owner/repo", "owner-repo"},
		{"Org/My-Tool", "org-my-tool"},
		{"a/b", "a-b"},
	}
	for _, tt := range tests {
		got := Slugify(tt.input)
		if got != tt.expected {
			t.Errorf("Slugify(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestRenderToolMarkdown(t *testing.T) {
	cfg := testCategoriesConfig()
	repo := db.Repo{
		Name:                "exploit-tool",
		FullName:            "owner/exploit-tool",
		HTMLURL:             "https://github.com/owner/exploit-tool",
		Stars:               100,
		Language:            "Python",
		Topics:              "exploit",
		Description:         "An exploit tool",
		Source:              "github-topic",
		AISummary:           "This is a great exploit tool.",
		InstallInstructions: "pip install exploit-tool",
		FirstSeen:           time.Date(2026, 3, 21, 0, 0, 0, 0, time.UTC),
	}

	md := RenderToolMarkdown(repo, cfg)

	// Check frontmatter fields
	if !strings.Contains(md, `title: "exploit-tool"`) {
		t.Error("missing title")
	}
	if !strings.Contains(md, `date: 2026-03-21`) {
		t.Error("missing date")
	}
	if !strings.Contains(md, `github_url: "https://github.com/owner/exploit-tool"`) {
		t.Error("missing github_url")
	}
	if !strings.Contains(md, `stars: 100`) {
		t.Error("missing stars")
	}
	if !strings.Contains(md, "This is a great exploit tool.") {
		t.Error("missing AI summary in body")
	}
	if !strings.Contains(md, `install_instructions: "pip install exploit-tool"`) {
		t.Error("missing install instructions")
	}
	if !strings.Contains(md, "exploit") {
		t.Error("missing category")
	}
}

func TestEscape(t *testing.T) {
	if escape(`say "hello"`) != `say \"hello\"` {
		t.Error("should escape double quotes")
	}
	if escape("line1\nline2") != `line1\nline2` {
		t.Error("should escape newlines")
	}
}

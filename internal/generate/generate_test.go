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
		{"exploit topic", db.Repo{Topics: "exploit,python", Description: "An exploit framework"}, []string{"exploit"}},
		{"multiple categories", db.Repo{Topics: "pentesting,scanner", Description: "A pentest scanner"}, []string{"pentesting", "scanner"}},
		{"osint from description", db.Repo{Topics: "", Description: "OSINT reconnaissance tool"}, []string{"osint"}},
		{"default when no match", db.Repo{Topics: "golang", Description: "A utility library"}, []string{"security-tools"}},
		{"awesome list source", db.Repo{Topics: "", Description: "some tool", Source: "awesome-list"}, []string{"awesome-list"}},
		{"red team from name", db.Repo{Name: "redteam-toolkit", Topics: "", Description: "toolkit"}, []string{"red-team"}},
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
	if Slugify("Owner/Repo") != "owner-repo" {
		t.Error("should lowercase and replace /")
	}
}

func TestRenderToolMarkdown(t *testing.T) {
	cfg := testCategoriesConfig()
	repo := db.Repo{
		Name: "exploit-tool", FullName: "owner/exploit-tool", HTMLURL: "https://github.com/owner/exploit-tool",
		Stars: 100, Language: "Python", Topics: "exploit", Description: "An exploit tool",
		Source: "github-topic", AISummary: "This is a great exploit tool.", ReadmeRaw: "# Exploit Tool\n\nFull readme here.",
		FirstSeen: time.Date(2026, 3, 21, 0, 0, 0, 0, time.UTC),
	}

	md := RenderToolMarkdown(repo, cfg)

	checks := []string{
		`title: "exploit-tool"`,
		`date: 2026-03-21`,
		`github_url: "https://github.com/owner/exploit-tool"`,
		`stars: 100`,
		"**AI Summary:** This is a great exploit tool.",
		"# Exploit Tool",
		"Full readme here.",
	}
	for _, c := range checks {
		if !strings.Contains(md, c) {
			t.Errorf("missing: %s", c)
		}
	}
	// Should NOT contain install_instructions
	if strings.Contains(md, "install_instructions") {
		t.Error("should not have install_instructions")
	}
}

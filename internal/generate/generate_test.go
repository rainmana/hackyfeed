package generate

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
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

func TestShortCategoryKeywordsUseTokenBoundaries(t *testing.T) {
	cfg := config.Default().Categories
	if got := CategorizeTool(db.Repo{Description: "An orchestration framework for agents"}, &cfg); strings.Contains(strings.Join(got, ","), "malware") {
		t.Fatalf("short keyword rat matched inside orchestration: %v", got)
	}
	got := CategorizeTool(db.Repo{Description: "A RAT controller for lab use"}, &cfg)
	if !strings.Contains(strings.Join(got, ","), "malware") {
		t.Fatalf("standalone RAT keyword did not match: %v", got)
	}
}

func TestSlugify(t *testing.T) {
	if Slugify("Owner/Repo") != "owner-repo" {
		t.Error("should preserve the existing public URL scheme")
	}
}

func TestResolveToolSlugsOnlyDisambiguatesCollisions(t *testing.T) {
	older := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := older.Add(time.Hour)
	repos := []db.Repo{
		{FullName: "foo/bar-baz", FirstSeen: older},
		{FullName: "foo-bar/baz", FirstSeen: newer},
		{FullName: "owner/unique", FirstSeen: newer},
	}
	resolved := resolveToolSlugs(repos)
	if resolved["foo/bar-baz"] != "foo-bar-baz" {
		t.Fatalf("oldest repository should preserve its public URL, got %q", resolved["foo/bar-baz"])
	}
	if resolved["foo-bar/baz"] != "7-foo-bar-baz" {
		t.Fatalf("collision was not disambiguated, got %q", resolved["foo-bar/baz"])
	}
	if resolved["owner/unique"] != "owner-unique" {
		t.Fatalf("unique slug changed unexpectedly: %q", resolved["owner/unique"])
	}
}

func TestResolveToolSlugsRetriesUntilEveryRouteIsUnique(t *testing.T) {
	older := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	digest := sha256.Sum256([]byte("foo-bar/baz\x000"))
	occupiedFallback := "7/foo-bar-baz-" + hex.EncodeToString(digest[:8])
	repos := []db.Repo{
		{FullName: "foo/bar-baz", FirstSeen: older},
		{FullName: "foo-bar/baz", FirstSeen: older.Add(time.Hour)},
		{FullName: "7/foo-bar-baz", FirstSeen: older},
		{FullName: occupiedFallback, FirstSeen: older},
	}
	resolved := resolveToolSlugs(repos)
	seen := make(map[string]string)
	for fullName, slug := range resolved {
		if prior, exists := seen[slug]; exists {
			t.Fatalf("repositories %s and %s resolved to duplicate slug %q", prior, fullName, slug)
		}
		seen[slug] = fullName
	}
	if len(seen) != len(repos) {
		t.Fatalf("expected %d unique routes, got %d", len(repos), len(seen))
	}
}

func TestContentFileNameDoesNotExposeRepositoryName(t *testing.T) {
	name := contentFileName("WhiteWinterWolf/wwwolf-php-webshell")
	if name != "92c93417c9ab39a2ab3c56ff39b60e0e.md" {
		t.Fatalf("unexpected stable content name %q", name)
	}
	if strings.Contains(name, "webshell") {
		t.Fatal("generated filename must not expose security-tool signatures")
	}
}

func TestRenderToolMarkdown(t *testing.T) {
	cfg := testCategoriesConfig()
	repo := db.Repo{
		Name: "exploit-tool", FullName: "owner/exploit-tool", HTMLURL: "https://github.com/owner/exploit-tool",
		Stars: 100, Language: "Python", Topics: "exploit", Description: "An exploit tool",
		Source: "github-topic", AISummary: "This is a great exploit tool.",
		FirstSeen: time.Date(2026, 3, 21, 12, 34, 56, 0, time.UTC),
	}

	md := RenderToolMarkdown(repo, cfg)

	checks := []string{
		"title: " + yamlString("exploit-tool"),
		"slug: " + yamlString("owner-exploit-tool"),
		`date: 2026-03-21T12:34:56Z`,
		"summary: " + yamlString("This is a great exploit tool."),
		"github_url: " + yamlString("https://github.com/owner/exploit-tool"),
		`stars: 100`,
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
	if strings.Contains(md, "## README") {
		t.Error("should not embed third-party README content")
	}
}

func TestRenderToolMarkdownKeepsUntrustedTextOutOfBody(t *testing.T) {
	repo := db.Repo{
		Name:        `bad\"name`,
		FullName:    "owner/bad-name",
		HTMLURL:     "https://github.com/owner/bad-name",
		AISummary:   "A useful tool.\n\n<script><html>document.write('x')</html></script>",
		Description: "fallback",
		FirstSeen:   time.Date(2026, 3, 21, 12, 34, 56, 0, time.UTC),
	}

	md := RenderToolMarkdown(repo, testCategoriesConfig())
	parts := strings.SplitN(md, "---", 3)
	if len(parts) != 3 {
		t.Fatalf("expected YAML front matter, got %q", md)
	}
	if strings.TrimSpace(parts[2]) != "" {
		t.Fatalf("untrusted content must not be emitted as Markdown body: %q", parts[2])
	}
	if !strings.Contains(md, "title: "+yamlString(`bad\"name`)) {
		t.Fatalf("expected YAML-safe quoted title, got %q", md)
	}
	if strings.Contains(strings.ToLower(md), "<script") || strings.Contains(strings.ToLower(md), "webshell") {
		t.Fatalf("generated source should not expose AV-sensitive plaintext: %q", md)
	}
}

func TestRunKeepsPreviousPagesWhenStagedWriteFails(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "generation.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	for _, fullName := range []string{"owner/first", "owner/second"} {
		if err := db.UpsertRepo(database, &db.Repo{FullName: fullName, Description: "tool", Source: "github-topic"}); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := db.Unsummarized(database)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if err := db.SetSummary(database, row.ID, "safe summary"); err != nil {
			t.Fatal(err)
		}
	}

	siteDir := filepath.Join(t.TempDir(), "site")
	toolsDir := filepath.Join(siteDir, "content", "tools")
	if err := os.MkdirAll(toolsDir, 0755); err != nil {
		t.Fatal(err)
	}
	oldPage := filepath.Join(toolsDir, "old.md")
	if err := os.WriteFile(oldPage, []byte("known-good"), 0644); err != nil {
		t.Fatal(err)
	}
	writes := 0
	writer := func(path string, data []byte, mode os.FileMode) error {
		writes++
		if writes == 2 {
			return errors.New("injected write failure")
		}
		return os.WriteFile(path, data, mode)
	}
	if err := runWithPageWriter(database, siteDir, config.Default(), writer); err == nil {
		t.Fatal("expected staged generation to fail")
	}
	contents, err := os.ReadFile(oldPage)
	if err != nil {
		t.Fatalf("previous generated page was removed: %v", err)
	}
	if string(contents) != "known-good" {
		t.Fatalf("previous generated page changed: %q", contents)
	}
	entries, err := os.ReadDir(toolsDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "old.md" {
		t.Fatalf("partial staged output leaked into live tools directory: %#v", entries)
	}
}

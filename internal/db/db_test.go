package db

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOpenCreatesTable(t *testing.T) {
	f, _ := os.CreateTemp("", "hackyfeed-test-*.db")
	f.Close()
	defer os.Remove(f.Name())

	db, err := Open(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM repos").Scan(&count)
	if err != nil {
		t.Fatalf("table should exist: %v", err)
	}
}

func TestNormalizeSummaryRejectsEmptyAndBoundsLength(t *testing.T) {
	if _, err := NormalizeSummary(" \n\t "); err == nil {
		t.Fatal("expected whitespace-only summary to fail")
	}
	normalized, err := NormalizeSummary(strings.Repeat("a", MaxSummaryRunes+50))
	if err != nil {
		t.Fatal(err)
	}
	if len([]rune(normalized)) != MaxSummaryRunes || !strings.HasSuffix(normalized, "...") {
		t.Fatalf("summary was not bounded correctly: %d runes", len([]rune(normalized)))
	}
}

func TestOpenPreservesLegacyReadmesUntilExplicitPurge(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	database, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := UpsertRepo(database, &Repo{FullName: "owner/tool", Owner: "owner", Name: "tool", HTMLURL: "https://github.com/owner/tool"}); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE repos SET readme_raw='legacy body' WHERE full_name='owner/tool'`); err != nil {
		t.Fatal(err)
	}
	database.Close()

	database, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	var body string
	if err := database.QueryRow(`SELECT readme_raw FROM repos WHERE full_name='owner/tool'`).Scan(&body); err != nil {
		t.Fatal(err)
	}
	if body != "legacy body" {
		t.Fatalf("opening the database changed legacy data: %q", body)
	}
	purged, err := PurgeReadmes(database)
	if err != nil {
		t.Fatal(err)
	}
	if purged != 1 {
		t.Fatalf("expected one explicit purge, got %d", purged)
	}
}

func TestUpsertAndQuery(t *testing.T) {
	f, _ := os.CreateTemp("", "hackyfeed-test-*.db")
	f.Close()
	defer os.Remove(f.Name())

	database, _ := Open(f.Name())
	defer database.Close()

	repo := &Repo{FullName: "owner/repo", Owner: "owner", Name: "repo", Description: "A test repo", HTMLURL: "https://github.com/owner/repo", Stars: 42, Language: "Go", Topics: "pentesting,exploit", LastPushed: time.Now(), Source: "github-topic"}
	if err := UpsertRepo(database, repo); err != nil {
		t.Fatal(err)
	}

	unsummarized, _ := Unsummarized(database)
	if len(unsummarized) != 1 || unsummarized[0].Stars != 42 {
		t.Fatalf("expected 1 repo with 42 stars, got %d", len(unsummarized))
	}
}

func TestUpsertUpdatesExisting(t *testing.T) {
	f, _ := os.CreateTemp("", "hackyfeed-test-*.db")
	f.Close()
	defer os.Remove(f.Name())

	database, _ := Open(f.Name())
	defer database.Close()

	UpsertRepo(database, &Repo{FullName: "owner/repo", Owner: "owner", Name: "repo", Description: "v1", HTMLURL: "https://github.com/owner/repo", Stars: 10, Source: "github-topic"})
	UpsertRepo(database, &Repo{FullName: "owner/repo", Owner: "owner", Name: "repo", Description: "v2", HTMLURL: "https://github.com/owner/repo", Stars: 100, Source: "github-topic"})

	unsummarized, _ := Unsummarized(database)
	if len(unsummarized) != 1 || unsummarized[0].Stars != 100 {
		t.Fatal("upsert should update stars")
	}
}

func TestAwesomeListUpsertDoesNotEraseGitHubMetadata(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "metadata.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	pushed := time.Date(2026, 7, 1, 2, 3, 4, 0, time.UTC)
	if err := UpsertRepo(database, &Repo{
		FullName: "owner/tool", Description: "GitHub description", Stars: 42,
		Language: "Go", Topics: "security,scanner", LastPushed: pushed, Source: "github-topic",
	}); err != nil {
		t.Fatal(err)
	}
	if err := UpsertRepo(database, &Repo{
		FullName: "owner/tool", Description: "Awesome-list description", Source: "awesome-list",
	}); err != nil {
		t.Fatal(err)
	}

	var description, language, topics, source string
	var stars int
	var lastPushed time.Time
	if err := database.QueryRow(`SELECT description, stars, language, topics, last_pushed, source FROM repos WHERE full_name=?`, "owner/tool").Scan(
		&description, &stars, &language, &topics, &lastPushed, &source,
	); err != nil {
		t.Fatal(err)
	}
	if description != "GitHub description" || stars != 42 || language != "Go" || topics != "security,scanner" || source != "github-topic" {
		t.Fatalf("awesome-list overlap erased GitHub metadata: description=%q stars=%d language=%q topics=%q source=%q", description, stars, language, topics, source)
	}
	if !lastPushed.UTC().Equal(pushed) {
		t.Fatalf("last_pushed changed: got %v, want %v", lastPushed, pushed)
	}
}

func TestUpsertRepoRejectsInvalidIdentity(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "identity.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := UpsertRepo(database, &Repo{FullName: "owner/tool/tree/main"}); err == nil {
		t.Fatal("expected invalid repository identity to be rejected")
	}
}

func TestUpsertRepoMergesCaseVariantIdentity(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "case.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := UpsertRepo(database, &Repo{FullName: "OWASP/Amass", Stars: 10, Source: "github-topic"}); err != nil {
		t.Fatal(err)
	}
	if err := UpsertRepo(database, &Repo{FullName: "owasp/amass", Description: "awesome", Source: "awesome-list"}); err != nil {
		t.Fatal(err)
	}
	if count, err := Count(database); err != nil || count != 1 {
		t.Fatalf("expected case-insensitive identity merge, count=%d err=%v", count, err)
	}
}

func TestSummaryWorkflow(t *testing.T) {
	f, _ := os.CreateTemp("", "hackyfeed-test-*.db")
	f.Close()
	defer os.Remove(f.Name())

	database, _ := Open(f.Name())
	defer database.Close()

	UpsertRepo(database, &Repo{FullName: "owner/tool", Owner: "owner", Name: "tool", Description: "desc", HTMLURL: "https://github.com/owner/tool", Stars: 50, Source: "github-topic"})

	unsummarized, _ := Unsummarized(database)
	if len(unsummarized) != 1 {
		t.Fatal("should be unsummarized")
	}

	if err := SetSummary(database, unsummarized[0].ID, "A great tool"); err != nil {
		t.Fatal(err)
	}

	unsummarized, _ = Unsummarized(database)
	if len(unsummarized) != 0 {
		t.Fatal("should no longer be unsummarized")
	}

	summarized, err := AllSummarized(database)
	if err != nil {
		t.Fatal(err)
	}
	if len(summarized) != 1 || summarized[0].AISummary != "A great tool" {
		t.Fatal("should be available to generation after summarization")
	}

	if err := IntegrityCheck(database); err != nil {
		t.Fatalf("database should pass integrity check: %v", err)
	}
}

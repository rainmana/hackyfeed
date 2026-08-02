package db

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCatalogRoundTrip(t *testing.T) {
	source, err := Open(t.TempDir() + "/source.db")
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()

	repo := &Repo{
		FullName: "owner/tool", Owner: "owner", Name: "tool",
		Description: "A useful tool", HTMLURL: "https://github.com/owner/tool",
		Stars: 42, Language: "Go", Topics: "pentesting,scanner", Source: "github-topic",
	}
	if err := UpsertRepo(source, repo); err != nil {
		t.Fatal(err)
	}
	unsummarized, err := Unsummarized(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := SetSummary(source, unsummarized[0].ID, "Safe summary with a newline\nand <script> text."); err != nil {
		t.Fatal(err)
	}

	var catalog bytes.Buffer
	count, err := ExportCatalog(source, &catalog)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 || strings.Count(strings.TrimSpace(catalog.String()), "\n") != 0 {
		t.Fatalf("expected one JSONL record, got %q", catalog.String())
	}

	target, err := Open(t.TempDir() + "/target.db")
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	imported, err := ImportCatalog(target, bytes.NewReader(catalog.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if imported != 1 {
		t.Fatalf("expected one imported record, got %d", imported)
	}

	restored, err := AllSummarized(target)
	if err != nil {
		t.Fatal(err)
	}
	if len(restored) != 1 || restored[0].FullName != repo.FullName || restored[0].Stars != 42 {
		t.Fatalf("unexpected restored repository: %#v", restored)
	}
	if restored[0].AISummary != "Safe summary with a newline and <script> text." {
		t.Fatalf("summary changed during round trip: %q", restored[0].AISummary)
	}
}

func TestImportCatalogIsAtomic(t *testing.T) {
	database, err := Open(t.TempDir() + "/catalog.db")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	valid := `{"full_name":"owner/tool","first_seen":"2026-03-21T12:34:56Z","ai_summary":"summary"}`
	invalid := `{"full_name":"not-a-repository","first_seen":"2026-03-21T12:34:56Z","ai_summary":"summary"}`
	if _, err := ImportCatalog(database, strings.NewReader(valid+"\n"+invalid+"\n")); err == nil {
		t.Fatal("expected invalid catalog to fail")
	}
	count, err := Count(database)
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("failed import must roll back, got %d rows", count)
	}
}

func TestNormalizeCatalogEntryRequiresTimestampAndSummary(t *testing.T) {
	entry := CatalogEntry{FullName: "owner/tool", FirstSeen: time.Time{}, AISummary: "summary"}
	if err := normalizeCatalogEntry(&entry); err == nil {
		t.Fatal("expected missing timestamp to fail")
	}
	entry.FirstSeen = time.Now()
	entry.AISummary = ""
	if err := normalizeCatalogEntry(&entry); err == nil {
		t.Fatal("expected missing summary to fail")
	}
}

func TestImportCatalogPreservesNewerLocalState(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	if err := UpsertRepo(database, &Repo{
		FullName: "owner/tool", Owner: "owner", Name: "tool",
		Description: "fresh description", HTMLURL: "https://github.com/owner/tool",
		Stars: 100, Language: "Go", Source: "github-topic",
	}); err != nil {
		t.Fatal(err)
	}
	unsummarized, err := Unsummarized(database)
	if err != nil {
		t.Fatal(err)
	}
	if err := SetSummary(database, unsummarized[0].ID, "fresh local summary"); err != nil {
		t.Fatal(err)
	}

	stale := `{"full_name":"owner/tool","owner":"attacker","name":"wrong","description":"stale description","html_url":"javascript:alert(1)","stars":10,"first_seen":"2026-03-21T12:34:56Z","ai_summary":"stale catalog summary"}`
	if _, err := ImportCatalog(database, strings.NewReader(stale)); err != nil {
		t.Fatal(err)
	}

	repos, err := AllSummarized(database)
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 1 {
		t.Fatalf("expected one repository, got %d", len(repos))
	}
	got := repos[0]
	if got.AISummary != "fresh local summary" || got.Description != "fresh description" || got.Stars != 100 {
		t.Fatalf("catalog overwrote newer local state: %#v", got)
	}
	if got.Owner != "owner" || got.Name != "tool" || got.HTMLURL != "https://github.com/owner/tool" {
		t.Fatalf("catalog identity was not canonicalized: %#v", got)
	}
}

func TestImportCatalogRepairsWhitespaceOnlyLegacySummary(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "legacy-summary.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := UpsertRepo(database, &Repo{FullName: "owner/tool"}); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE repos SET ai_summary='   ' WHERE full_name='owner/tool'`); err != nil {
		t.Fatal(err)
	}
	catalog := `{"full_name":"owner/tool","first_seen":"2026-03-21T12:34:56Z","ai_summary":"restored summary"}`
	if _, err := ImportCatalog(database, strings.NewReader(catalog)); err != nil {
		t.Fatal(err)
	}
	repos, err := AllSummarized(database)
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 1 || repos[0].AISummary != "restored summary" {
		t.Fatalf("whitespace-only legacy summary was not repaired: %#v", repos)
	}
}

func TestCheckedInCatalogSeed(t *testing.T) {
	seedPath := filepath.Clean(filepath.Join("..", "..", "data", "catalog.jsonl"))
	seed, err := os.ReadFile(seedPath)
	if err != nil {
		t.Fatal(err)
	}
	manifestBytes, err := os.ReadFile(filepath.Join(filepath.Dir(seedPath), "catalog.manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest CatalogManifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	if err := VerifyCatalogManifest(seed, manifest); err != nil {
		t.Fatal(err)
	}

	database, err := Open(filepath.Join(t.TempDir(), "seed.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	imported, err := ImportCatalog(database, bytes.NewReader(seed))
	if err != nil {
		t.Fatal(err)
	}
	if imported != manifest.Records {
		t.Fatalf("expected %d seed records, got %d", manifest.Records, imported)
	}
	summarized, err := SummarizedCount(database)
	if err != nil {
		t.Fatal(err)
	}
	if summarized != manifest.Records {
		t.Fatalf("expected all %d seed records to be summarized, got %d", manifest.Records, summarized)
	}
	if err := IntegrityCheck(database); err != nil {
		t.Fatal(err)
	}
}

func TestCatalogManifestDetectsTruncation(t *testing.T) {
	data := []byte("{\"full_name\":\"owner/tool\"}\n")
	manifest := NewCatalogManifest(data, 1)
	if err := VerifyCatalogManifest(data, manifest); err != nil {
		t.Fatal(err)
	}
	if err := VerifyCatalogManifest(data[:len(data)-2], manifest); err == nil {
		t.Fatal("expected truncated catalog to fail verification")
	}
	manifest.Records = 2
	if err := VerifyCatalogManifest(data, manifest); err == nil {
		t.Fatal("expected incorrect record count to fail verification")
	}
}

func TestCanonicalGitHubIdentityBoundsComponents(t *testing.T) {
	validOwner := strings.Repeat("a", 39)
	validRepo := strings.Repeat("b", 100)
	if _, _, _, err := CanonicalGitHubIdentity(validOwner + "/" + validRepo); err != nil {
		t.Fatalf("expected boundary identity to be valid: %v", err)
	}
	for _, fullName := range []string{
		strings.Repeat("a", 40) + "/repo",
		"owner/" + strings.Repeat("b", 101),
		"-owner/repo",
		"owner-/repo",
		"owner/.",
		"owner/..",
	} {
		if _, _, _, err := CanonicalGitHubIdentity(fullName); err == nil {
			t.Errorf("expected %q to be rejected", fullName)
		}
	}
}

func TestCatalogBoundsExternalMetadataAndStillRoundTrips(t *testing.T) {
	source, err := Open(filepath.Join(t.TempDir(), "source.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	if err := UpsertRepo(source, &Repo{
		FullName: "owner/tool", Description: strings.Repeat("<&>", 2000),
		Language: strings.Repeat("G", 1000), Topics: strings.Repeat("topic,", 2000), Source: "awesome-list",
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := Unsummarized(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := SetSummary(source, rows[0].ID, "bounded metadata"); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if _, err := ExportCatalog(source, &output); err != nil {
		t.Fatal(err)
	}
	if output.Len() >= 100_000 {
		t.Fatalf("bounded one-record catalog unexpectedly large: %d bytes", output.Len())
	}
	target, err := Open(filepath.Join(t.TempDir(), "target.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	if _, err := ImportCatalog(target, bytes.NewReader(output.Bytes())); err != nil {
		t.Fatal(err)
	}
	restored, err := AllSummarized(target)
	if err != nil {
		t.Fatal(err)
	}
	if len([]rune(restored[0].Description)) != MaxDescriptionRunes {
		t.Fatalf("description limit not applied: %d runes", len([]rune(restored[0].Description)))
	}
}

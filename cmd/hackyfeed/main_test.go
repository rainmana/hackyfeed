package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/rainmana/hackyfeed/internal/db"
)

func TestWriteFileAtomicReplacesExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.jsonl")
	if err := writeFileAtomic(path, []byte("first\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(path, []byte("second\n"), 0644); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "second\n" {
		t.Fatalf("unexpected contents %q", contents)
	}
}

func TestParseRestoreArgsRejectsTyposBeforeRestore(t *testing.T) {
	if allowEmpty, err := parseRestoreArgs(nil); err != nil || allowEmpty {
		t.Fatalf("unexpected default parse result: allow=%v err=%v", allowEmpty, err)
	}
	if allowEmpty, err := parseRestoreArgs([]string{"--allow-empty"}); err != nil || !allowEmpty {
		t.Fatalf("expected --allow-empty to be accepted: allow=%v err=%v", allowEmpty, err)
	}
	for _, arguments := range [][]string{{"--typo"}, {"--allow-empty", "extra"}} {
		if _, err := parseRestoreArgs(arguments); err == nil {
			t.Fatalf("expected %q to be rejected", arguments)
		}
	}
}

func TestRestoreCatalogStateRepairsPartiallyPopulatedDatabase(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	if err := db.UpsertRepo(database, &db.Repo{
		FullName: "owner/unsummarized", Owner: "owner", Name: "unsummarized",
		HTMLURL: "https://github.com/owner/unsummarized", Source: "github-topic",
	}); err != nil {
		t.Fatal(err)
	}

	catalogPath := filepath.Join(t.TempDir(), "catalog.jsonl")
	catalog := []byte("{\"full_name\":\"owner/restored\",\"first_seen\":\"2026-03-21T12:34:56Z\",\"ai_summary\":\"restored summary\"}\n")
	if err := os.WriteFile(catalogPath, catalog, 0644); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	if err := restoreCatalogState(database, server.URL, catalogPath, false); err != nil {
		t.Fatal(err)
	}

	total, err := db.Count(database)
	if err != nil {
		t.Fatal(err)
	}
	summarized, err := db.SummarizedCount(database)
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || summarized != 1 {
		t.Fatalf("expected partial state plus restored summary, got total=%d summarized=%d", total, summarized)
	}
}

func TestAutomaticRestoreDoesNotUndoReset(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.UpsertRepo(database, &db.Repo{
		FullName: "owner/tool", Owner: "owner", Name: "tool", HTMLURL: "https://github.com/owner/tool",
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := db.Unsummarized(database)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetSummary(database, rows[0].ID, "summary to reset"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ResetSummaries(database); err != nil {
		t.Fatal(err)
	}

	catalogPath := filepath.Join(t.TempDir(), "catalog.jsonl")
	if err := os.WriteFile(catalogPath, []byte("{\"full_name\":\"owner/tool\",\"first_seen\":\"2026-03-21T12:34:56Z\",\"ai_summary\":\"old summary\"}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := restoreCatalogIfEmpty(database, "https://example.invalid/", catalogPath); err != nil {
		t.Fatal(err)
	}
	summarized, err := db.SummarizedCount(database)
	if err != nil {
		t.Fatal(err)
	}
	if summarized != 0 {
		t.Fatalf("automatic restore undid reset for %d records", summarized)
	}
}

func TestStrictRemoteRestoreRejectsCorruptPublishedCatalog(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		response.Write([]byte(`{"schema_version":1,"records":1,"sha256":"not-the-catalog-hash"}`))
	}))
	defer server.Close()
	database, err := db.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	if err := restoreCatalogState(database, server.URL, filepath.Join(t.TempDir(), "missing.jsonl"), true); err == nil {
		t.Fatal("expected strict restore to reject corrupt published state")
	}
}

func TestStrictRemoteRestoreMergesHistoryBeyondSeed(t *testing.T) {
	remoteCatalog := []byte("{\"full_name\":\"remote/new-tool\",\"first_seen\":\"2026-04-01T12:34:56Z\",\"ai_summary\":\"remote summary\"}\n")
	manifest := db.NewCatalogManifest(remoteCatalog, 1)
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/catalog.manifest.json" {
			response.Write(manifestBytes)
			return
		}
		if request.URL.Path == "/catalog.jsonl" {
			response.Write(remoteCatalog)
			return
		}
		http.NotFound(response, request)
	}))
	defer server.Close()
	database, err := db.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.UpsertRepo(database, &db.Repo{
		FullName: "local/survivor", Owner: "local", Name: "survivor", HTMLURL: "https://github.com/local/survivor",
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := db.Unsummarized(database)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetSummary(database, rows[0].ID, "local summary"); err != nil {
		t.Fatal(err)
	}

	if err := restoreCatalogState(database, server.URL, filepath.Join(t.TempDir(), "missing.jsonl"), true); err != nil {
		t.Fatal(err)
	}
	count, err := db.SummarizedCount(database)
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("expected local and remote history, got %d summaries", count)
	}
}

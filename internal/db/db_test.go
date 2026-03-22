package db

import (
	"os"
	"testing"
	"time"
)

func testDB(t *testing.T) *testHelper {
	t.Helper()
	f, err := os.CreateTemp("", "hackyfeed-test-*.db")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	db, err := Open(f.Name())
	if err != nil {
		os.Remove(f.Name())
		t.Fatal(err)
	}
	return &testHelper{db: db, path: f.Name(), t: t}
}

type testHelper struct {
	db   interface{ Close() error }
	path string
	t    *testing.T
}

func TestOpenCreatesTable(t *testing.T) {
	f, _ := os.CreateTemp("", "hackyfeed-test-*.db")
	f.Close()
	defer os.Remove(f.Name())

	db, err := Open(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Verify table exists by querying it
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM repos").Scan(&count)
	if err != nil {
		t.Fatalf("table should exist: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 rows, got %d", count)
	}
}

func TestUpsertAndQuery(t *testing.T) {
	f, _ := os.CreateTemp("", "hackyfeed-test-*.db")
	f.Close()
	defer os.Remove(f.Name())

	database, _ := Open(f.Name())
	defer database.Close()

	repo := &Repo{
		FullName:    "owner/repo",
		Owner:       "owner",
		Name:        "repo",
		Description: "A test repo",
		HTMLURL:     "https://github.com/owner/repo",
		Stars:       42,
		Language:    "Go",
		Topics:      "pentesting,exploit",
		LastPushed:  time.Now(),
		Source:      "github-topic",
	}

	if err := UpsertRepo(database, repo); err != nil {
		t.Fatal(err)
	}

	// Should appear in unsummarized
	unsummarized, err := Unsummarized(database)
	if err != nil {
		t.Fatal(err)
	}
	if len(unsummarized) != 1 {
		t.Fatalf("expected 1 unsummarized, got %d", len(unsummarized))
	}
	if unsummarized[0].FullName != "owner/repo" {
		t.Fatalf("expected owner/repo, got %s", unsummarized[0].FullName)
	}
	if unsummarized[0].Stars != 42 {
		t.Fatalf("expected 42 stars, got %d", unsummarized[0].Stars)
	}
}

func TestUpsertUpdatesExisting(t *testing.T) {
	f, _ := os.CreateTemp("", "hackyfeed-test-*.db")
	f.Close()
	defer os.Remove(f.Name())

	database, _ := Open(f.Name())
	defer database.Close()

	repo := &Repo{FullName: "owner/repo", Owner: "owner", Name: "repo", Description: "v1", HTMLURL: "https://github.com/owner/repo", Stars: 10, Source: "github-topic"}
	UpsertRepo(database, repo)

	// Upsert again with updated stars
	repo.Stars = 100
	repo.Description = "v2"
	UpsertRepo(database, repo)

	unsummarized, _ := Unsummarized(database)
	if len(unsummarized) != 1 {
		t.Fatalf("expected 1 repo (deduped), got %d", len(unsummarized))
	}
	if unsummarized[0].Stars != 100 {
		t.Fatalf("expected 100 stars after upsert, got %d", unsummarized[0].Stars)
	}
}

func TestSummaryWorkflow(t *testing.T) {
	f, _ := os.CreateTemp("", "hackyfeed-test-*.db")
	f.Close()
	defer os.Remove(f.Name())

	database, _ := Open(f.Name())
	defer database.Close()

	repo := &Repo{FullName: "owner/tool", Owner: "owner", Name: "tool", Description: "desc", HTMLURL: "https://github.com/owner/tool", Stars: 50, Source: "github-topic"}
	UpsertRepo(database, repo)

	// Before summary: should be unsummarized, not unpublished (no summary yet)
	unsummarized, _ := Unsummarized(database)
	if len(unsummarized) != 1 {
		t.Fatal("should be unsummarized")
	}

	unpublished, _ := Unpublished(database)
	if len(unpublished) != 0 {
		t.Fatal("should not be unpublished yet (no summary)")
	}

	// Set summary
	SetSummary(database, unsummarized[0].ID, "A great tool", "pip install tool")

	// Now should be unpublished (has summary, not yet published)
	unsummarized, _ = Unsummarized(database)
	if len(unsummarized) != 0 {
		t.Fatal("should no longer be unsummarized")
	}

	unpublished, _ = Unpublished(database)
	if len(unpublished) != 1 {
		t.Fatal("should be unpublished")
	}
	if unpublished[0].AISummary != "A great tool" {
		t.Fatalf("expected summary, got %s", unpublished[0].AISummary)
	}

	// Mark published
	MarkPublished(database, unpublished[0].ID)

	unpublished, _ = Unpublished(database)
	if len(unpublished) != 0 {
		t.Fatal("should no longer be unpublished")
	}

	published, _ := AllPublished(database)
	if len(published) != 1 {
		t.Fatal("should be in published list")
	}
}

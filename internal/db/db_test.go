package db

import (
	"os"
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

	unpublished, _ := Unpublished(database)
	if len(unpublished) != 0 {
		t.Fatal("should not be unpublished yet")
	}

	SetSummary(database, unsummarized[0].ID, "A great tool", "# Full README content here")

	unsummarized, _ = Unsummarized(database)
	if len(unsummarized) != 0 {
		t.Fatal("should no longer be unsummarized")
	}

	unpublished, _ = Unpublished(database)
	if len(unpublished) != 1 || unpublished[0].AISummary != "A great tool" || unpublished[0].ReadmeRaw != "# Full README content here" {
		t.Fatal("should be unpublished with summary and readme")
	}

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

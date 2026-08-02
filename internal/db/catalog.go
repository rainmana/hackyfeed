package db

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"
)

const CatalogSchemaVersion = 1

type CatalogManifest struct {
	SchemaVersion int    `json:"schema_version"`
	Records       int    `json:"records"`
	SHA256        string `json:"sha256"`
}

func NewCatalogManifest(data []byte, records int) CatalogManifest {
	digest := sha256.Sum256(data)
	return CatalogManifest{
		SchemaVersion: CatalogSchemaVersion,
		Records:       records,
		SHA256:        fmt.Sprintf("%x", digest),
	}
}

func VerifyCatalogManifest(data []byte, manifest CatalogManifest) error {
	if manifest.SchemaVersion != CatalogSchemaVersion {
		return fmt.Errorf("unsupported catalog schema version %d", manifest.SchemaVersion)
	}
	if manifest.Records < 0 {
		return fmt.Errorf("invalid catalog record count %d", manifest.Records)
	}
	expected := NewCatalogManifest(data, manifest.Records)
	if !strings.EqualFold(manifest.SHA256, expected.SHA256) {
		return fmt.Errorf("catalog SHA-256 mismatch")
	}
	actualRecords := 0
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		if len(bytes.TrimSpace(line)) > 0 {
			actualRecords++
		}
	}
	if actualRecords != manifest.Records {
		return fmt.Errorf("catalog record count mismatch: manifest=%d actual=%d", manifest.Records, actualRecords)
	}
	return nil
}

type CatalogEntry struct {
	FullName    string    `json:"full_name"`
	Owner       string    `json:"owner"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	HTMLURL     string    `json:"html_url"`
	Stars       int       `json:"stars,omitempty"`
	Language    string    `json:"language,omitempty"`
	Topics      string    `json:"topics,omitempty"`
	FirstSeen   time.Time `json:"first_seen"`
	Source      string    `json:"source,omitempty"`
	AISummary   string    `json:"ai_summary"`
}

var (
	catalogOwnerPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?$`)
	catalogNamePattern  = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,100}$`)
)

const (
	MaxDescriptionRunes = 1000
	MaxLanguageRunes    = 100
	MaxTopicsRunes      = 4000
	MaxSourceRunes      = 100
)

// CanonicalGitHubIdentity validates a GitHub repository name and returns the
// identity fields stored throughout the catalog. Keeping this check in one
// place prevents discovery sources from creating rows that a later catalog
// export cannot restore.
func CanonicalGitHubIdentity(fullName string) (owner, name, htmlURL string, err error) {
	fullName = strings.TrimSpace(fullName)
	parts := strings.Split(fullName, "/")
	if len(parts) != 2 || !catalogOwnerPattern.MatchString(parts[0]) || !catalogNamePattern.MatchString(parts[1]) || parts[1] == "." || parts[1] == ".." {
		return "", "", "", fmt.Errorf("invalid full_name %q", fullName)
	}
	return parts[0], parts[1], "https://github.com/" + fullName, nil
}

func ExportCatalog(database *sql.DB, writer io.Writer) (int, error) {
	repos, err := AllSummarized(database)
	if err != nil {
		return 0, err
	}
	sort.Slice(repos, func(i, j int) bool {
		return repos[i].FullName < repos[j].FullName
	})

	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(true)
	for _, repo := range repos {
		summary, err := NormalizeSummary(repo.AISummary)
		if err != nil {
			return 0, fmt.Errorf("export %s: %w", repo.FullName, err)
		}
		entry := CatalogEntry{
			FullName:    repo.FullName,
			Owner:       repo.Owner,
			Name:        repo.Name,
			Description: NormalizeMetadata(repo.Description, MaxDescriptionRunes),
			HTMLURL:     repo.HTMLURL,
			Stars:       repo.Stars,
			Language:    NormalizeMetadata(repo.Language, MaxLanguageRunes),
			Topics:      NormalizeMetadata(repo.Topics, MaxTopicsRunes),
			FirstSeen:   repo.FirstSeen.UTC(),
			Source:      NormalizeMetadata(repo.Source, MaxSourceRunes),
			AISummary:   summary,
		}
		if err := encoder.Encode(entry); err != nil {
			return 0, err
		}
	}
	return len(repos), nil
}

func ImportCatalog(database *sql.DB, reader io.Reader) (int, error) {
	transaction, err := database.Begin()
	if err != nil {
		return 0, err
	}
	defer transaction.Rollback()

	statement, err := transaction.Prepare(`
		INSERT INTO repos (
			full_name, owner, name, description, html_url, stars, language,
			topics, first_seen, source, ai_summary, summarized_at, published
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, 0)
		ON CONFLICT(full_name) DO UPDATE SET
			owner=CASE WHEN TRIM(repos.owner) = '' THEN excluded.owner ELSE repos.owner END,
			name=CASE WHEN TRIM(repos.name) = '' THEN excluded.name ELSE repos.name END,
			description=CASE WHEN TRIM(repos.description) = '' THEN excluded.description ELSE repos.description END,
			html_url=CASE WHEN TRIM(repos.html_url) = '' THEN excluded.html_url ELSE repos.html_url END,
			stars=MAX(repos.stars, excluded.stars),
			language=CASE WHEN TRIM(repos.language) = '' THEN excluded.language ELSE repos.language END,
			topics=CASE WHEN TRIM(repos.topics) = '' THEN excluded.topics ELSE repos.topics END,
			first_seen=CASE WHEN excluded.first_seen < repos.first_seen THEN excluded.first_seen ELSE repos.first_seen END,
			source=CASE WHEN TRIM(repos.source) = '' THEN excluded.source ELSE repos.source END,
			ai_summary=CASE WHEN TRIM(repos.ai_summary) = '' THEN excluded.ai_summary ELSE repos.ai_summary END,
			summarized_at=CASE WHEN TRIM(repos.ai_summary) = '' THEN CURRENT_TIMESTAMP ELSE repos.summarized_at END
	`)
	if err != nil {
		return 0, err
	}
	defer statement.Close()

	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	count := 0
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var entry CatalogEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			return 0, fmt.Errorf("catalog line %d: %w", lineNumber, err)
		}
		if err := normalizeCatalogEntry(&entry); err != nil {
			return 0, fmt.Errorf("catalog line %d: %w", lineNumber, err)
		}
		var existingFullName string
		lookupErr := transaction.QueryRow(`SELECT full_name FROM repos WHERE LOWER(full_name)=LOWER(?) LIMIT 1`, entry.FullName).Scan(&existingFullName)
		if lookupErr == nil {
			entry.FullName = existingFullName
			entry.Owner, entry.Name, entry.HTMLURL, err = CanonicalGitHubIdentity(existingFullName)
			if err != nil {
				return 0, fmt.Errorf("catalog line %d: existing identity: %w", lineNumber, err)
			}
		} else if lookupErr != sql.ErrNoRows {
			return 0, fmt.Errorf("catalog line %d: identity lookup: %w", lineNumber, lookupErr)
		}
		if _, err := statement.Exec(
			entry.FullName, entry.Owner, entry.Name, entry.Description,
			entry.HTMLURL, entry.Stars, entry.Language, entry.Topics,
			entry.FirstSeen.UTC(), entry.Source, entry.AISummary,
		); err != nil {
			return 0, fmt.Errorf("catalog line %d: %w", lineNumber, err)
		}
		count++
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}
	if err := transaction.Commit(); err != nil {
		return 0, err
	}
	return count, nil
}

func normalizeCatalogEntry(entry *CatalogEntry) error {
	entry.FullName = strings.TrimSpace(entry.FullName)
	owner, name, htmlURL, err := CanonicalGitHubIdentity(entry.FullName)
	if err != nil {
		return err
	}
	if entry.Stars < 0 {
		return fmt.Errorf("negative stars for %q", entry.FullName)
	}
	entry.Owner = owner
	entry.Name = name
	entry.HTMLURL = htmlURL
	entry.Description = NormalizeMetadata(entry.Description, MaxDescriptionRunes)
	entry.Language = NormalizeMetadata(entry.Language, MaxLanguageRunes)
	entry.Topics = NormalizeMetadata(entry.Topics, MaxTopicsRunes)
	entry.Source = NormalizeMetadata(entry.Source, MaxSourceRunes)
	if entry.FirstSeen.IsZero() {
		return fmt.Errorf("missing first_seen for %q", entry.FullName)
	}
	summary, err := NormalizeSummary(entry.AISummary)
	if err != nil {
		return fmt.Errorf("invalid ai_summary for %q: %w", entry.FullName, err)
	}
	entry.AISummary = summary
	return nil
}

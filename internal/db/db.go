package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
	"unicode"

	_ "modernc.org/sqlite"
)

type Repo struct {
	ID           int64
	FullName     string
	Owner        string
	Name         string
	Description  string
	HTMLURL      string
	Stars        int
	Language     string
	Topics       string // comma-separated
	LastPushed   time.Time
	FirstSeen    time.Time
	Source       string
	AISummary    string
	SummarizedAt sql.NullTime
}

func Open(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	_, err = db.Exec(`
	CREATE TABLE IF NOT EXISTS repos (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		full_name TEXT UNIQUE NOT NULL,
		owner TEXT NOT NULL,
		name TEXT NOT NULL,
		description TEXT DEFAULT '',
		html_url TEXT NOT NULL,
		stars INTEGER DEFAULT 0,
		language TEXT DEFAULT '',
		topics TEXT DEFAULT '',
		last_pushed DATETIME,
		first_seen DATETIME DEFAULT CURRENT_TIMESTAMP,
		source TEXT DEFAULT 'github-topic',
		ai_summary TEXT DEFAULT '',
		readme_raw TEXT DEFAULT '',
		summarized_at DATETIME,
		published BOOLEAN DEFAULT 0
	);
	CREATE INDEX IF NOT EXISTS idx_repos_published ON repos(published);
	CREATE INDEX IF NOT EXISTS idx_repos_source ON repos(source);
	`)
	if err != nil {
		db.Close()
		return nil, err
	}
	// Keep compatibility with databases created by early versions. Legacy
	// README bodies are never queried for generation or exported; removing them
	// is an explicit maintenance operation so opening a DB stays non-destructive.
	if _, err := db.Exec(`ALTER TABLE repos ADD COLUMN readme_raw TEXT DEFAULT ''`); err != nil {
		// SQLite reports a duplicate-column error for already-migrated databases.
		// Verify the column exists before treating the migration as successful.
		var count int
		if checkErr := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('repos') WHERE name='readme_raw'`).Scan(&count); checkErr != nil || count != 1 {
			db.Close()
			return nil, fmt.Errorf("migrate readme_raw column: %w", err)
		}
	}
	return db, nil
}

func UpsertRepo(db *sql.DB, r *Repo) error {
	owner, name, htmlURL, err := CanonicalGitHubIdentity(r.FullName)
	if err != nil {
		return err
	}
	if r.Stars < 0 {
		return fmt.Errorf("negative stars for %q", r.FullName)
	}
	r.Owner = owner
	r.Name = name
	r.HTMLURL = htmlURL
	var existingFullName string
	lookupErr := db.QueryRow(`SELECT full_name FROM repos WHERE LOWER(full_name)=LOWER(?) LIMIT 1`, r.FullName).Scan(&existingFullName)
	if lookupErr == nil {
		r.FullName = existingFullName
		r.Owner, r.Name, r.HTMLURL, err = CanonicalGitHubIdentity(existingFullName)
		if err != nil {
			return err
		}
	} else if lookupErr != sql.ErrNoRows {
		return lookupErr
	}
	r.Description = NormalizeMetadata(r.Description, MaxDescriptionRunes)
	r.Language = NormalizeMetadata(r.Language, MaxLanguageRunes)
	r.Topics = NormalizeMetadata(r.Topics, MaxTopicsRunes)
	r.Source = NormalizeMetadata(r.Source, MaxSourceRunes)
	if r.Source == "" {
		r.Source = "github-topic"
	}
	var lastPushed any
	if !r.LastPushed.IsZero() {
		lastPushed = r.LastPushed.UTC()
	}
	_, err = db.Exec(`
	INSERT INTO repos (full_name, owner, name, description, html_url, stars, language, topics, last_pushed, source)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(full_name) DO UPDATE SET
		owner=excluded.owner,
		name=excluded.name,
		description=CASE
			WHEN excluded.source='awesome-list' AND repos.source='github-topic' THEN repos.description
			WHEN excluded.description != '' THEN excluded.description
			ELSE repos.description
		END,
		html_url=excluded.html_url,
		stars=MAX(repos.stars, excluded.stars),
		language=CASE WHEN excluded.language != '' THEN excluded.language ELSE repos.language END,
		topics=CASE WHEN excluded.topics != '' THEN excluded.topics ELSE repos.topics END,
		last_pushed=COALESCE(excluded.last_pushed, repos.last_pushed),
		source=CASE
			WHEN excluded.source='awesome-list' AND repos.source='github-topic' THEN repos.source
			ELSE excluded.source
		END
	`, r.FullName, r.Owner, r.Name, r.Description, r.HTMLURL, r.Stars, r.Language, r.Topics, lastPushed, r.Source)
	return err
}

func Unsummarized(db *sql.DB) ([]Repo, error) {
	rows, err := db.Query(`SELECT id, full_name, owner, name, description, html_url, stars, language, topics FROM repos WHERE TRIM(ai_summary) = '' ORDER BY stars DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var repos []Repo
	for rows.Next() {
		var r Repo
		if err := rows.Scan(&r.ID, &r.FullName, &r.Owner, &r.Name, &r.Description, &r.HTMLURL, &r.Stars, &r.Language, &r.Topics); err != nil {
			return nil, err
		}
		repos = append(repos, r)
	}
	return repos, rows.Err()
}

func SetSummary(db *sql.DB, id int64, summary string) error {
	normalized, err := NormalizeSummary(summary)
	if err != nil {
		return err
	}
	_, err = db.Exec(`UPDATE repos SET ai_summary=?, readme_raw='', summarized_at=CURRENT_TIMESTAMP WHERE id=?`, normalized, id)
	return err
}

const MaxSummaryRunes = 1200

func NormalizeMetadata(value string, maxRunes int) string {
	sanitized := strings.Map(func(character rune) rune {
		switch character {
		case '\n', '\r', '\t':
			return ' '
		default:
			if unicode.IsControl(character) {
				return -1
			}
			return character
		}
	}, value)
	normalized := strings.Join(strings.Fields(strings.TrimSpace(sanitized)), " ")
	runes := []rune(normalized)
	if maxRunes > 0 && len(runes) > maxRunes {
		if maxRunes <= 3 {
			return string(runes[:maxRunes])
		}
		return string(runes[:maxRunes-3]) + "..."
	}
	return normalized
}

func NormalizeSummary(summary string) (string, error) {
	sanitized := strings.Map(func(character rune) rune {
		switch character {
		case '\n', '\r', '\t':
			return ' '
		default:
			if unicode.IsControl(character) {
				return -1
			}
			return character
		}
	}, summary)
	normalized := strings.Join(strings.Fields(strings.TrimSpace(sanitized)), " ")
	if normalized == "" {
		return "", fmt.Errorf("summary is empty")
	}
	runes := []rune(normalized)
	if len(runes) > MaxSummaryRunes {
		normalized = string(runes[:MaxSummaryRunes-3]) + "..."
	}
	return normalized, nil
}

// ResetSummaries clears all summaries so they can be regenerated
func ResetSummaries(db *sql.DB) (int64, error) {
	r, err := db.Exec(`UPDATE repos SET ai_summary='', readme_raw='', summarized_at=NULL, published=0`)
	if err != nil {
		return 0, err
	}
	return r.RowsAffected()
}

func PurgeReadmes(db *sql.DB) (int64, error) {
	result, err := db.Exec(`UPDATE repos SET readme_raw='' WHERE readme_raw != ''`)
	if err != nil {
		return 0, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if count > 0 {
		if _, err := db.Exec(`VACUUM`); err != nil {
			return count, fmt.Errorf("compact database after README purge: %w", err)
		}
	}
	return count, nil
}

func AllSummarized(db *sql.DB) ([]Repo, error) {
	rows, err := db.Query(`SELECT id, full_name, owner, name, description, html_url, stars, language, topics, first_seen, source, ai_summary FROM repos WHERE TRIM(ai_summary) != '' ORDER BY first_seen DESC, full_name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var repos []Repo
	for rows.Next() {
		var r Repo
		if err := rows.Scan(&r.ID, &r.FullName, &r.Owner, &r.Name, &r.Description, &r.HTMLURL, &r.Stars, &r.Language, &r.Topics, &r.FirstSeen, &r.Source, &r.AISummary); err != nil {
			return nil, err
		}
		repos = append(repos, r)
	}
	return repos, rows.Err()
}

func Count(db *sql.DB) (int, error) {
	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM repos`).Scan(&count)
	return count, err
}

func SummarizedCount(db *sql.DB) (int, error) {
	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM repos WHERE TRIM(ai_summary) != ''`).Scan(&count)
	return count, err
}

func IntegrityCheck(db *sql.DB) error {
	var result string
	if err := db.QueryRow(`PRAGMA quick_check`).Scan(&result); err != nil {
		return err
	}
	if result != "ok" {
		return fmt.Errorf("SQLite quick_check returned %q", result)
	}
	return nil
}

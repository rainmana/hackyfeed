package db

import (
	"database/sql"
	"time"

	_ "modernc.org/sqlite"
)

type Repo struct {
	ID                  int64
	FullName            string
	Owner               string
	Name                string
	Description         string
	HTMLURL             string
	Stars               int
	Language            string
	Topics              string // comma-separated
	LastPushed          time.Time
	FirstSeen           time.Time
	Source              string // "github-topic" or "awesome-rainmana"
	AISummary           string
	InstallInstructions string
	SummarizedAt        sql.NullTime
	Published           bool
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
		install_instructions TEXT DEFAULT '',
		summarized_at DATETIME,
		published BOOLEAN DEFAULT 0
	);
	CREATE INDEX IF NOT EXISTS idx_repos_published ON repos(published);
	CREATE INDEX IF NOT EXISTS idx_repos_source ON repos(source);
	`)
	return db, err
}

func UpsertRepo(db *sql.DB, r *Repo) error {
	_, err := db.Exec(`
	INSERT INTO repos (full_name, owner, name, description, html_url, stars, language, topics, last_pushed, source)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(full_name) DO UPDATE SET
		description=excluded.description, stars=excluded.stars, language=excluded.language,
		topics=excluded.topics, last_pushed=excluded.last_pushed
	`, r.FullName, r.Owner, r.Name, r.Description, r.HTMLURL, r.Stars, r.Language, r.Topics, r.LastPushed, r.Source)
	return err
}

func Unsummarized(db *sql.DB) ([]Repo, error) {
	rows, err := db.Query(`SELECT id, full_name, owner, name, description, html_url, stars, language, topics FROM repos WHERE ai_summary = '' ORDER BY stars DESC`)
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

func SetSummary(db *sql.DB, id int64, summary, install string) error {
	_, err := db.Exec(`UPDATE repos SET ai_summary=?, install_instructions=?, summarized_at=CURRENT_TIMESTAMP WHERE id=?`, summary, install, id)
	return err
}

func Unpublished(db *sql.DB) ([]Repo, error) {
	rows, err := db.Query(`SELECT id, full_name, owner, name, description, html_url, stars, language, topics, first_seen, source, ai_summary, install_instructions FROM repos WHERE ai_summary != '' AND published = 0 ORDER BY stars DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var repos []Repo
	for rows.Next() {
		var r Repo
		if err := rows.Scan(&r.ID, &r.FullName, &r.Owner, &r.Name, &r.Description, &r.HTMLURL, &r.Stars, &r.Language, &r.Topics, &r.FirstSeen, &r.Source, &r.AISummary, &r.InstallInstructions); err != nil {
			return nil, err
		}
		repos = append(repos, r)
	}
	return repos, rows.Err()
}

func MarkPublished(db *sql.DB, id int64) error {
	_, err := db.Exec(`UPDATE repos SET published=1 WHERE id=?`, id)
	return err
}

func AllPublished(db *sql.DB) ([]Repo, error) {
	rows, err := db.Query(`SELECT id, full_name, owner, name, description, html_url, stars, language, topics, first_seen, source, ai_summary, install_instructions FROM repos WHERE published = 1 ORDER BY first_seen DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var repos []Repo
	for rows.Next() {
		var r Repo
		if err := rows.Scan(&r.ID, &r.FullName, &r.Owner, &r.Name, &r.Description, &r.HTMLURL, &r.Stars, &r.Language, &r.Topics, &r.FirstSeen, &r.Source, &r.AISummary, &r.InstallInstructions); err != nil {
			return nil, err
		}
		repos = append(repos, r)
	}
	return repos, rows.Err()
}

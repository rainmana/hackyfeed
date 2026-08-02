package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rainmana/hackyfeed/internal/config"
	"github.com/rainmana/hackyfeed/internal/db"
	"github.com/rainmana/hackyfeed/internal/fetch"
	"github.com/rainmana/hackyfeed/internal/generate"
	"github.com/rainmana/hackyfeed/internal/summarize"
)

var errPublishedCatalogNotFound = errors.New("published catalog manifest not found")

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: hackyfeed <command>")
		fmt.Println("Commands: fetch, summarize, generate, all, restore, catalog-import, catalog-export, doctor, reset, purge-readmes")
		os.Exit(1)
	}

	cfgPath := envOr("HACKYFEED_CONFIG", "hackyfeed.toml")
	cfg, err := config.Load(cfgPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	dbPath := envOr("HACKYFEED_DB", "hackyfeed.db")
	siteDir := envOr("HACKYFEED_SITE", "site")
	catalogPath := envOr("HACKYFEED_CATALOG", filepath.Join("data", "catalog.jsonl"))

	database, err := db.Open(dbPath)
	if err != nil {
		log.Fatalf("db open: %v", err)
	}
	defer database.Close()

	switch os.Args[1] {
	case "fetch":
		if err := restoreCatalogIfEmpty(database, cfg.Site.BaseURL, catalogPath); err != nil {
			log.Fatalf("restore: %v", err)
		}
		token := os.Getenv("GITHUB_TOKEN")
		if token == "" {
			log.Println("[warn] GITHUB_TOKEN not set, API rate limits will be low")
		}
		if err := fetch.Run(database, token, &cfg.Fetch); err != nil {
			log.Fatalf("fetch: %v", err)
		}

	case "summarize":
		if err := restoreCatalogIfEmpty(database, cfg.Site.BaseURL, catalogPath); err != nil {
			log.Fatalf("restore: %v", err)
		}
		llm := summarize.LLMConfig{
			APIBase: envOr("LLM_API_BASE", "http://localhost:4000/v1"),
			APIKey:  os.Getenv("LLM_API_KEY"),
			Model:   envOr("LLM_MODEL", "gpt-4o-mini"),
		}
		if err := summarize.Run(database, llm, &cfg.Summarize); err != nil {
			log.Fatalf("summarize: %v", err)
		}

	case "generate":
		if err := restoreCatalogIfEmpty(database, cfg.Site.BaseURL, catalogPath); err != nil {
			log.Fatalf("restore: %v", err)
		}
		if err := generate.Run(database, siteDir, cfg); err != nil {
			log.Fatalf("generate: %v", err)
		}
		if err := exportCatalog(database, filepath.Join(siteDir, "static", "catalog.jsonl")); err != nil {
			log.Fatalf("catalog export: %v", err)
		}

	case "all":
		if err := restoreCatalogIfEmpty(database, cfg.Site.BaseURL, catalogPath); err != nil {
			log.Fatalf("restore: %v", err)
		}
		token := os.Getenv("GITHUB_TOKEN")
		log.Println("=== fetch ===")
		if err := fetch.Run(database, token, &cfg.Fetch); err != nil {
			log.Fatalf("fetch: %v", err)
		}
		log.Println("=== summarize ===")
		llm := summarize.LLMConfig{
			APIBase: envOr("LLM_API_BASE", "http://localhost:4000/v1"),
			APIKey:  os.Getenv("LLM_API_KEY"),
			Model:   envOr("LLM_MODEL", "gpt-4o-mini"),
		}
		if err := summarize.Run(database, llm, &cfg.Summarize); err != nil {
			log.Fatalf("summarize: %v", err)
		}
		log.Println("=== generate ===")
		if err := generate.Run(database, siteDir, cfg); err != nil {
			log.Fatalf("generate: %v", err)
		}
		if err := exportCatalog(database, filepath.Join(siteDir, "static", "catalog.jsonl")); err != nil {
			log.Fatalf("catalog export: %v", err)
		}
		log.Println("=== done ===")

	case "restore":
		allowEmpty, err := parseRestoreArgs(os.Args[2:])
		if err != nil {
			log.Fatalf("restore: %v", err)
		}
		if err := restoreCatalogState(database, cfg.Site.BaseURL, catalogPath, true); err != nil {
			log.Fatalf("restore: %v", err)
		}
		count, err := db.SummarizedCount(database)
		if err != nil {
			log.Fatalf("restore: %v", err)
		}
		if count == 0 && !allowEmpty {
			log.Fatalf("restore: no catalog state was available; use --allow-empty only when bootstrapping a new feed")
		}

	case "catalog-import":
		path := catalogPath
		if len(os.Args) > 2 {
			path = os.Args[2]
		}
		count, err := importCatalogFile(database, path)
		if err != nil {
			log.Fatalf("catalog import: %v", err)
		}
		log.Printf("Imported %d catalog records from %s", count, path)

	case "catalog-export":
		path := catalogPath
		if len(os.Args) > 2 {
			path = os.Args[2]
		}
		if err := exportCatalog(database, path); err != nil {
			log.Fatalf("catalog export: %v", err)
		}

	case "doctor":
		if err := db.IntegrityCheck(database); err != nil {
			log.Fatalf("database integrity: %v", err)
		}
		count, err := db.Count(database)
		if err != nil {
			log.Fatalf("database count: %v", err)
		}
		log.Printf("Database OK (%d repositories)", count)

	case "reset":
		count, err := db.ResetSummaries(database)
		if err != nil {
			log.Fatalf("reset: %v", err)
		}
		log.Printf("Reset %d repos — they will be re-summarized on next run", count)

	case "purge-readmes":
		count, err := db.PurgeReadmes(database)
		if err != nil {
			log.Fatalf("purge readmes: %v", err)
		}
		log.Printf("Purged legacy README bodies from %d repositories", count)

	default:
		fmt.Printf("Unknown command: %s\n", os.Args[1])
		os.Exit(1)
	}
}

func parseRestoreArgs(arguments []string) (bool, error) {
	if len(arguments) == 0 {
		return false, nil
	}
	if len(arguments) == 1 && arguments[0] == "--allow-empty" {
		return true, nil
	}
	return false, fmt.Errorf("unknown restore options %q", arguments)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func restoreCatalogIfEmpty(database *sql.DB, baseURL, localPath string) error {
	count, err := db.Count(database)
	if err != nil {
		return err
	}
	if count > 0 {
		log.Printf("[restore] database already contains %d repositories; use the restore command to merge recovery state", count)
		return nil
	}
	return restoreCatalogState(database, baseURL, localPath, false)
}

func restoreCatalogState(database *sql.DB, baseURL, localPath string, strictRemote bool) error {
	before, err := db.SummarizedCount(database)
	if err != nil {
		return err
	}

	// A clean or explicitly refreshed database also merges the last catalog
	// published with the site. Import it before the fallback seed so the newest
	// published summaries win on a clean restore; existing DB state wins both.
	if before == 0 || strictRemote {
		remoteURL := strings.TrimRight(baseURL, "/") + "/catalog.jsonl"
		remoteCount, remoteErr := importCatalogURL(database, remoteURL)
		if remoteErr != nil {
			if strictRemote && !errors.Is(remoteErr, errPublishedCatalogNotFound) {
				return fmt.Errorf("published catalog: %w", remoteErr)
			}
			log.Printf("[restore] published catalog unavailable: %v", remoteErr)
		} else {
			log.Printf("[restore] merged %d records from %s", remoteCount, remoteURL)
		}
	}

	localCount, err := importCatalogFile(database, localPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("local catalog: %w", err)
	}
	if err == nil {
		log.Printf("[restore] merged %d records from %s", localCount, localPath)
	}

	after, err := db.SummarizedCount(database)
	if err != nil {
		return err
	}
	if after == 0 {
		log.Printf("[restore] no prior catalog found; starting with an empty database")
	} else {
		log.Printf("[restore] %d summarized repositories available", after)
	}
	return nil
}

func importCatalogFile(database *sql.DB, path string) (int, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	manifestBody, manifestErr := os.ReadFile(catalogManifestPath(path))
	if manifestErr == nil {
		manifest, err := decodeCatalogManifest(manifestBody)
		if err != nil {
			return 0, err
		}
		if err := db.VerifyCatalogManifest(body, manifest); err != nil {
			return 0, err
		}
	} else if !os.IsNotExist(manifestErr) {
		return 0, manifestErr
	}
	return db.ImportCatalog(database, bytes.NewReader(body))
}

func importCatalogURL(database *sql.DB, catalogURL string) (int, error) {
	const maxCatalogBytes = 50 * 1024 * 1024
	const maxManifestBytes = 1024 * 1024
	client := &http.Client{Timeout: 30 * time.Second}
	manifestBody, err := downloadCatalogFile(client, catalogManifestPath(catalogURL), maxManifestBytes)
	if err != nil {
		return 0, err
	}
	manifest, err := decodeCatalogManifest(manifestBody)
	if err != nil {
		return 0, err
	}
	body, err := downloadCatalogFile(client, catalogURL, maxCatalogBytes)
	if err != nil {
		if errors.Is(err, errPublishedCatalogNotFound) {
			return 0, fmt.Errorf("catalog file is missing despite a published manifest")
		}
		return 0, err
	}
	if err := db.VerifyCatalogManifest(body, manifest); err != nil {
		return 0, err
	}
	return db.ImportCatalog(database, bytes.NewReader(body))
}

func downloadCatalogFile(client *http.Client, location string, maxBytes int64) ([]byte, error) {
	response, err := client.Get(location)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("%w: %s", errPublishedCatalogNotFound, location)
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s returned HTTP %d", location, response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxBytes {
		return nil, fmt.Errorf("%s exceeds %d bytes", location, maxBytes)
	}
	return body, nil
}

func decodeCatalogManifest(data []byte) (db.CatalogManifest, error) {
	var manifest db.CatalogManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return manifest, fmt.Errorf("decode catalog manifest: %w", err)
	}
	return manifest, nil
}

func catalogManifestPath(catalogPath string) string {
	extension := filepath.Ext(catalogPath)
	if extension == "" {
		return catalogPath + ".manifest.json"
	}
	return strings.TrimSuffix(catalogPath, extension) + ".manifest.json"
}

func exportCatalog(database *sql.DB, path string) error {
	var output bytes.Buffer
	count, err := db.ExportCatalog(database, &output)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	if err := writeFileAtomic(path, output.Bytes(), 0644); err != nil {
		return err
	}
	manifest := db.NewCatalogManifest(output.Bytes(), count)
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	manifestBytes = append(manifestBytes, '\n')
	if err := writeFileAtomic(catalogManifestPath(path), manifestBytes, 0644); err != nil {
		return err
	}
	log.Printf("[catalog] exported %d records to %s", count, path)
	return nil
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) (returnErr error) {
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, "."+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() {
		temporary.Close()
		if removeErr := os.Remove(temporaryPath); removeErr != nil && !os.IsNotExist(removeErr) && returnErr == nil {
			returnErr = removeErr
		}
	}()

	if _, err := temporary.Write(data); err != nil {
		return err
	}
	if err := temporary.Chmod(mode); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := replaceFile(temporaryPath, path); err != nil {
		return err
	}
	return nil
}

func replaceFile(temporaryPath, targetPath string) error {
	if err := os.Rename(temporaryPath, targetPath); err == nil {
		return nil
	} else if _, statErr := os.Stat(targetPath); os.IsNotExist(statErr) {
		return err
	} else if statErr != nil {
		return statErr
	}

	directory := filepath.Dir(targetPath)
	backup, err := os.CreateTemp(directory, "."+filepath.Base(targetPath)+"-backup-*")
	if err != nil {
		return err
	}
	backupPath := backup.Name()
	if err := backup.Close(); err != nil {
		os.Remove(backupPath)
		return err
	}
	if err := os.Remove(backupPath); err != nil {
		return err
	}
	if err := os.Rename(targetPath, backupPath); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, targetPath); err != nil {
		if rollbackErr := os.Rename(backupPath, targetPath); rollbackErr != nil {
			return fmt.Errorf("replace target: %v; rollback failed: %w", err, rollbackErr)
		}
		return err
	}
	if err := os.Remove(backupPath); err != nil {
		return fmt.Errorf("remove replaced file backup: %w", err)
	}
	return nil
}

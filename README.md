# HackyFeed

[![CI](https://github.com/rainmana/hackyfeed/actions/workflows/ci.yml/badge.svg)](https://github.com/rainmana/hackyfeed/actions/workflows/ci.yml)
[![Update & Deploy](https://github.com/rainmana/hackyfeed/actions/workflows/daily-update.yml/badge.svg)](https://github.com/rainmana/hackyfeed/actions/workflows/daily-update.yml)
[RSS feed](https://rainmana.github.io/hackyfeed/feed.xml)

HackyFeed is a self-updating GitHub Pages catalog for discovering cybersecurity tools. It searches GitHub, summarizes new repositories through any OpenAI-compatible API, renders a Hugo site, and publishes a small, standards-friendly RSS feed.

The repository is also intended to be forked into other focused catalogs, including an LLM tooling feed.

## How it works

```text
GitHub API ──> SQLite ──> AI summary ──> Hugo pages ──> GitHub Pages
                 │                            │
                 └──── public catalog ────────┘
```

Third-party READMEs are transient input to the summarizer. They are not stored in the catalog and are never copied into the site. Generated pages contain bounded, YAML-quoted metadata only, Hugo's raw HTML renderer remains disabled, and generation stages a complete replacement before changing the current tool collection.

The CLI commands are:

| Command | Purpose |
| --- | --- |
| `hackyfeed fetch` | Discover and update repositories from GitHub topics and awesome lists. |
| `hackyfeed summarize` | Summarize repositories that do not yet have a summary. |
| `hackyfeed generate` | Generate Hugo pages and the public recovery catalog. |
| `hackyfeed all` | Run fetch, summarize, and generate. |
| `hackyfeed restore` | Merge the last published catalog and the checked-in seed into SQLite. Use `--allow-empty` only for a brand-new feed. |
| `hackyfeed doctor` | Run a SQLite integrity check and report the repository count. |
| `hackyfeed catalog-import [path]` | Import a catalog JSONL file. |
| `hackyfeed catalog-export [path]` | Export summarized public state as JSONL. |
| `hackyfeed reset` | Clear summaries so they can be regenerated. |
| `hackyfeed purge-readmes` | Explicitly remove legacy stored README bodies from an older database. |

## Quick start

Requirements:

- Go 1.25.6 or the compatible version declared in `go.mod`
- Hugo Extended 0.164.0
- A GitHub token for higher API limits
- An OpenAI-compatible endpoint for summarization

Build and restore the known catalog:

```bash
go build -o hackyfeed ./cmd/hackyfeed/
./hackyfeed restore
./hackyfeed generate
hugo server --source site --config hugo.toml,hackyfeed.generated.toml
```

To perform a full update, set the environment variables described below and run:

```bash
./hackyfeed all
```

On Windows, the built executable is `hackyfeed.exe`; set environment variables in PowerShell rather than using the Unix-style `.env` loading pattern.

## RSS

The feed is published at [`/feed.xml`](https://rainmana.github.io/hackyfeed/feed.xml) and advertised from every page.

Its contract is intentionally narrow:

- only tool pages are included;
- newest discoveries appear first with full UTC timestamps;
- links and GUIDs are absolute and stable;
- descriptions contain only bounded plain-text AI summaries;
- the feed is limited to the newest 50 tools;
- About, Disclaimer, README content, scripts, and relative links are excluded.

The Hugo integration test builds a deliberately hostile sample with `--minify`, parses the resulting feed, and enforces that contract.

## Durable state and recovery

SQLite is the fast working database, not the only copy of the catalog.

- `data/catalog.jsonl` is a checked-in recovery seed containing 1,070 historical summaries, including 74 entries recovered from the last successful live deployment.
- `data/catalog.manifest.json` records the schema version, record count, and SHA-256 digest; restore rejects mismatched published state.
- Every successful generation publishes a refreshed catalog and matching manifest with the site.
- A clean run restores the newest published catalog first, then fills any gaps from the seed.
- GitHub Actions cache preserves in-progress work, including summaries completed before a later failure, but it is treated as disposable.

Catalogs contain bounded public repository metadata and AI summaries only. Raw README bodies and credentials are excluded. Legacy cached README bodies are removed explicitly with `purge-readmes` so simply opening a local database remains non-destructive.

## Configuration and reuse

Discovery, categorization, and summarization are configured in `hackyfeed.toml`. The file is required, unknown keys fail validation, and an explicit `[categories.rules]` table replaces the cybersecurity defaults instead of merging with them.

For an LLM tooling catalog, a starting point could be:

```toml
[fetch]
topics = [
  "llm",
  "large-language-models",
  "ai-agents",
  "mcp",
  "rag",
  "prompt-engineering",
]
min_stars = 10

[summarize]
system_prompt = """You catalog LLM developer tools. Given a repository README, write a concise technical summary covering the tool's purpose, intended workflow, and distinguishing capability. Respond with summary text only."""

[categories]
default_category = "llm-tools"

[categories.rules]
agents = ["agent", "multi-agent", "orchestration"]
mcp = ["mcp", "model-context-protocol"]
rag = ["rag", "retrieval", "vector-database"]
evaluation = ["eval", "benchmark", "observability"]
```

When creating a new feed, update:

1. `[site]` in `hackyfeed.toml`.
2. Topic, category, and prompt rules for the new domain.
3. The Curated Sources, About, and Disclaimer pages under `site/content/`.
4. GitHub Pages settings, repository secrets, and the optional `LLM_MODEL` Actions variable.
5. The recovery seed and manifest: export the new catalog instead of reusing HackyFeed's cybersecurity data.

`hackyfeed generate` writes `site/hackyfeed.generated.toml`, making `[site]` the authoritative branding and canonical-URL configuration. The checked-in `site/hugo.toml` contains structural Hugo settings and deliberately generic fallback identity only.

See [the template guide](docs/template-guide.md) for the extraction checklist and the design boundaries that should remain intact.

## Environment variables

| Variable | Description | Required |
| --- | --- | --- |
| `GITHUB_TOKEN` | GitHub token used for discovery. | Recommended |
| `LLM_API_BASE` | Base URL of an OpenAI-compatible API. | For summarization |
| `LLM_API_KEY` | API key for the summarization endpoint. | Endpoint-dependent |
| `LLM_MODEL` | Model name; defaults to `gpt-4o-mini`. | No |
| `HACKYFEED_DB` | SQLite path; defaults to `hackyfeed.db`. | No |
| `HACKYFEED_SITE` | Hugo site directory; defaults to `site`. | No |
| `HACKYFEED_CONFIG` | TOML configuration path; defaults to `hackyfeed.toml`. | No |
| `HACKYFEED_CATALOG` | Recovery seed path; defaults to `data/catalog.jsonl`. | No |

## GitHub Actions

The deployment workflow behaves differently by trigger to make updates predictable:

- a push to `main` tests and republishes known catalog state without calling the LLM;
- scheduled and manual runs also fetch and summarize new repositories;
- Hugo is pinned, Go follows `go.mod`, and every reusable action is pinned to a full commit SHA;
- the database cache is explicitly saved even if a later build or deployment step fails.

Configure these repository secrets:

- `LLM_API_BASE`
- `LLM_API_KEY`
- `GH_PAT` (optional; the workflow falls back to `github.token`)

Set the repository's Pages source to **GitHub Actions**. Public repositories can have scheduled workflows paused after prolonged inactivity; a push still runs the deployment workflow.

## Changelog

### 2026-08-02 — Recovery, security, and reliability overhaul

This release repairs the original end-to-end pipeline and replaces the fragile generated-state model that caused repeated GitHub Pages failures.

#### Safe content generation

- Removed third-party README bodies from the persistent repository model, public catalog, generated pages, and RSS output.
- Restricted published content to bounded repository metadata and bounded AI summaries.
- Kept Hugo's unsafe raw HTML renderer disabled instead of weakening minification to accommodate hostile upstream content.
- Encoded generated front matter defensively so arbitrary repository text cannot alter page structure or trigger local security scanners.
- Changed tool generation to stage a complete replacement before swapping it into place, preventing partial sites after an interrupted run.
- Added stable SHA-256-based source filenames and deterministic URL-slug collision handling while preserving existing non-conflicting URLs.
- Added full RFC 3339 UTC timestamps and token-aware category matching.
- Moved generated branding and canonical URL settings behind the required `[site]` configuration.

#### Durable catalog and recovery

- Added deterministic JSONL catalog import and export with transactional merging.
- Added a versioned manifest containing the record count and SHA-256 digest; corrupt or mismatched published catalogs now fail closed.
- Added atomic catalog replacement with Windows-safe backup and rollback behavior.
- Recovered 74 entries that existed only on the last successful live deployment.
- Checked in a recovery seed containing 1,070 summarized repositories so a clean runner can reproduce the complete site without an old Actions cache.
- Preserved newer local state during catalog merges and normalized repository identities case-insensitively.
- Added explicit legacy README purging without making ordinary database opening destructive.

#### Discovery and summarization

- Changed GitHub discovery ordering to surface recently updated qualifying repositories instead of repeatedly scanning only the most-starred results.
- Canonicalized awesome-list GitHub URLs, bounded response sizes, and made empty or unusable source responses fail visibly.
- Aggregated source failures while allowing useful sources to complete.
- Prevented lower-quality awesome-list metadata from overwriting richer GitHub API metadata.
- Distinguished definitive README absence from transient GitHub failures so temporary outages remain retryable.
- Added bounded, rune-safe README and LLM response handling.
- Added request, response, malformed endpoint, protocol, and persistence error handling throughout the summarizer.
- Added a service-failure circuit breaker that preserves completed work without allowing a few repository-specific failures to starve the remaining queue.

#### RSS and Hugo site

- Replaced the broken default RSS output with a custom feed containing only the newest 50 tool entries.
- Added stable absolute permalinks and GUIDs, complete publication dates, categories, and safe plain-text descriptions.
- Excluded static pages, raw README content, scripts, and relative links from the feed.
- Advertised the feed from every rendered page.
- Filtered the home page and taxonomies to use the explicit generated tool model.
- Renamed the awesome-list-facing site section to the domain-neutral **Curated Sources** for reuse by other catalogs.

#### CLI, configuration, and automation

- Added `restore`, `doctor`, `catalog-import`, `catalog-export`, and `purge-readmes` commands.
- Made configuration files required, rejected unknown TOML keys, and added semantic validation for URLs, limits, categories, and summarization settings.
- Made explicit category-rule tables replace defaults, which prevents domain templates from accidentally inheriting cybersecurity matches.
- Added a dedicated CI workflow for tests, vetting, catalog restoration, generation, and a production Hugo build.
- Rebuilt the Pages workflow so pushes safely republish known state while scheduled and manual runs perform discovery and summarization.
- Reactivated the scheduled workflow after GitHub's inactivity pause and added `main` pushes as a deterministic republish path.
- Pinned Hugo and every reusable GitHub Action to explicit versions or full commit SHAs.
- Replaced secret-shaped example credentials with inert placeholders that do not trigger push-protection scanners.
- Made the SQLite cache resumable but non-authoritative, with public and checked-in catalogs as durable state.
- Added Pages artifact checks and preserved the database cache even when a later deployment step fails.

#### Testing, analysis, and reuse

- Added coverage for configuration, catalog integrity, network failures, retry behavior, queue progress, hostile metadata, atomic generation, slug collisions, and recovery orchestration.
- Added a real Hugo integration test that builds hostile sample content with minification and validates the resulting RSS feed.
- Verified every recovered route and the complete 1,070-page production build.
- Indexed and queried the final Go codebase with Joern to confirm that fetched README content has no dataflow path into generated page writes.
- Added a detailed [recovery journal](docs/recovery-journal.md) for the future article and a [template extraction guide](docs/template-guide.md) for building feeds in other domains.

## Validation

Run the full suite, including the real Hugo integration test:

```bash
HUGO_BIN=hugo go test ./...
go vet ./...
hugo --source site --config hugo.toml,hackyfeed.generated.toml --minify
```

CI also imports the checked-in seed, generates all tool pages, and performs a production Hugo build.

## Project structure

```text
cmd/hackyfeed/       CLI and recovery orchestration
internal/config/     TOML configuration
internal/db/         SQLite and public catalog import/export
internal/fetch/      GitHub topics and awesome-list discovery
internal/summarize/  README-to-summary pipeline
internal/generate/   Safe Hugo page generation and RSS integration tests
data/                Checked-in recovery seed
site/                Hugo site and custom RSS template
docs/                Recovery journal and template notes
.github/workflows/   CI and Pages deployment
```

## Disclaimer

HackyFeed is an automated aggregator; inclusion is not endorsement. Use listed tools only with authorization and in accordance with applicable law. See the [full disclaimer](https://rainmana.github.io/hackyfeed/pages/disclaimer/).

## License

MIT

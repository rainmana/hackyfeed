# HackyFeed

A cybersecurity tools aggregator inspired by [KitPloit](https://www.kitploit.com). Automatically discovers security tools from GitHub, generates AI summaries, and publishes a static site via Hugo + GitHub Pages.

**Fork this repo and edit `hackyfeed.toml` to create your own tool aggregator for any topic.**

## How It Works

```
GitHub API ──→ SQLite DB ──→ AI Summaries ──→ Hugo Markdown ──→ GitHub Pages
  (topics)      (diff)       (LiteLLM)        (generate)        (deploy)
```

A single Go CLI tool handles the entire pipeline:

- **`hackyfeed fetch`** — Searches GitHub for repos by topic and parses awesome lists. Upserts into SQLite.
- **`hackyfeed summarize`** — Fetches READMEs and generates summaries via any OpenAI-compatible API.
- **`hackyfeed generate`** — Renders Hugo markdown from the database.
- **`hackyfeed all`** — Runs fetch → summarize → generate in sequence.

## Quick Start

### Prerequisites

- Go 1.22+
- Hugo (extended)
- A GitHub personal access token
- An OpenAI-compatible API endpoint (LiteLLM, OpenAI, Ollama, etc.)

### Setup

```bash
git clone https://github.com/rainmana/hackyfeed.git
cd hackyfeed
cp .env.example .env
# Edit .env with your tokens
```

### Build & Run Locally

```bash
go build -o hackyfeed ./cmd/hackyfeed/
export $(grep -v '^#' .env | xargs)
./hackyfeed all
cd site && hugo server
```

### Run Tests

```bash
go test ./... -v
```

## Customization

The entire pipeline is driven by `hackyfeed.toml`. Fork this repo and edit it to aggregate anything:

### Change Topics

```toml
[fetch]
topics = [
  "machine-learning",
  "deep-learning",
  "llm",
  "transformer",
]
min_stars = 50
```

### Add Your Own Awesome Lists

```toml
[fetch]
awesome_lists = [
  "https://raw.githubusercontent.com/yourname/awesome-list/main/README.md",
]
```

### Define Custom Categories

```toml
[categories]
default_category = "ai-tools"

[categories.rules]
nlp = ["nlp", "natural-language", "text-processing"]
vision = ["computer-vision", "image", "object-detection"]
llm = ["llm", "large-language-model", "gpt", "transformer"]
```

### Customize AI Summaries

```toml
[summarize]
system_prompt = """You are an AI/ML tools cataloger. Given a README, produce JSON with:
- "summary": 2-3 sentence description for a technical audience.
- "install": Primary installation method.
Respond ONLY with valid JSON."""
max_readme_chars = 4000
```

### Site Branding

```toml
[site]
title = "ML Feed"
description = "Discover the latest machine learning tools from GitHub."
author = "yourname"
```

Then update `site/hugo.toml` to match your site title/URL.

## Environment Variables

| Variable | Description | Required |
|----------|-------------|----------|
| `GITHUB_TOKEN` | GitHub PAT with `public_repo` scope | Recommended |
| `LLM_API_BASE` | OpenAI-compatible API base URL | For summarize |
| `LLM_API_KEY` | API key for the LLM endpoint | For summarize |
| `LLM_MODEL` | Model name (default: `gpt-4o-mini`) | No |
| `HACKYFEED_DB` | SQLite DB path (default: `hackyfeed.db`) | No |
| `HACKYFEED_SITE` | Hugo site directory (default: `site`) | No |
| `HACKYFEED_CONFIG` | Config file path (default: `hackyfeed.toml`) | No |

## GitHub Actions

The included workflow runs daily at 8am MT (2pm UTC). Set these repo secrets:

- `GH_PAT` — GitHub personal access token
- `LLM_API_BASE` — Your LLM API endpoint
- `LLM_API_KEY` — Your LLM API key

In repo Settings → Pages, set source to **GitHub Actions**.

## Project Structure

```
├── cmd/hackyfeed/       # CLI entry point
├── internal/
│   ├── config/          # TOML config loader
│   ├── db/              # SQLite schema & queries
│   ├── fetch/           # GitHub API + awesome list parser
│   ├── summarize/       # LLM-powered README summarization
│   └── generate/        # Hugo markdown generator
├── site/                # Hugo site
│   ├── content/         # Generated tool pages
│   └── themes/          # Terminal hacker theme (dark/light)
├── hackyfeed.toml       # ← Edit this to customize everything
├── .github/workflows/   # Daily automation
└── .env.example         # Environment variable template
```

## Disclaimer

HackyFeed is an aggregator — inclusion is not endorsement. All tools must be used ethically and legally. Nothing on this site represents the views of the maintainer's employer. See the [full disclaimer](https://rainmana.github.io/hackyfeed/pages/disclaimer/).

## License

MIT

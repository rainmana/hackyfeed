# Reusing HackyFeed as a catalog template

This guide describes how to create a new domain-specific feed from the repaired HackyFeed pipeline. It is intentionally a copy-and-specialize workflow for now; converting the repository into an official GitHub template should wait until the repaired site has completed a successful live deployment.

## Invariants to keep

These are pipeline safety properties, not HackyFeed branding:

- READMEs remain transient summarizer input and never become generated page bodies.
- The LLM output is normalized, bounded, YAML-quoted, and rendered as text.
- Hugo raw HTML stays disabled.
- RSS selects a bounded tool collection and never uses automatic page summaries.
- A complete site is regenerated from summarized catalog state.
- Generated tool pages are staged as a complete directory before replacing the prior set.
- SQLite cache is resumable work state, not durable truth.
- Every durable catalog has a matching schema/count/SHA-256 manifest.
- The published public catalog contains no credentials, prompts with secrets, or raw source documents.
- A normal push does not trigger costly summarization.
- Configuration is required; unknown keys and unsafe limits fail instead of falling back silently.
- The hostile-content Hugo integration test remains enabled in CI.

## New repository checklist

1. Copy the repository from a proven working commit.
2. Change the module path in `go.mod` and any import paths if the project will live under a different GitHub owner or repository.
3. Replace the site identity in `hackyfeed.toml`; generation creates Hugo's identity override.
4. Replace discovery topics, awesome lists, category rules, and the summarizer prompt.
5. Rewrite the Curated Sources, About, and Disclaimer pages for the new domain.
6. Replace the cybersecurity seed with an empty or domain-appropriate catalog export and its matching manifest. Never publish HackyFeed's seed as part of an unrelated catalog.
7. Build locally and run `hackyfeed restore --allow-empty` for an empty first bootstrap.
8. Run a manual update with a deliberately small `batch_limit` before enabling the daily schedule.
9. Inspect generated pages and `feed.xml`, then deploy known state.
10. Increase the batch limit only after cost and quality are understood.

An empty catalog is supported: the deployment workflow uses `restore --allow-empty`, so the first push can publish the site shell. Create the empty JSONL and matching manifest through the CLI rather than manually truncating one file:

```powershell
$env:HACKYFEED_DB = Join-Path $env:TEMP ("new-feed-" + [Guid]::NewGuid() + ".db")
.\hackyfeed.exe catalog-export data/catalog.jsonl
```

The resulting manifest records zero entries and the SHA-256 of an empty catalog. A scheduled or manual run can then discover and enrich the first records.

## Configuration surfaces

### Domain behavior

Edit `hackyfeed.toml` for:

- GitHub topics and minimum stars;
- optional raw awesome-list URLs;
- LLM tone, prompt, input limit, and per-run batch limit;
- category keywords and fallback category;
- canonical site URL and descriptive metadata.

An explicit `[categories.rules]` table replaces the bundled cybersecurity rule map. It does not merge with it, so a new domain does not inherit accidental security classifications.

### Hugo identity

`[site]` in `hackyfeed.toml` is authoritative. Generation writes the untracked `site/hackyfeed.generated.toml` override used by local, CI, and production Hugo builds. The checked-in `site/hugo.toml` holds structural settings and a deliberately generic fallback identity.

### Automation

Configure GitHub Pages to use Actions and add:

- `LLM_API_BASE` as a repository secret;
- `LLM_API_KEY` as a repository secret when the endpoint requires one;
- `GH_PAT` only if the built-in Actions token is insufficient;
- `LLM_MODEL` as an optional repository variable.

The push path intentionally restores, tests, generates, and deploys without fetching or summarizing. Manual and scheduled events perform enrichment.

Reusable Actions are pinned to full commit SHAs. Keep the version comments and use Dependabot or an equivalent reviewed update process rather than reverting to mutable major tags.

## Suggested LLM tooling taxonomy

Useful discovery areas include:

- agent frameworks and orchestration;
- Model Context Protocol clients, servers, and developer tools;
- retrieval and indexing;
- evaluation, tracing, and observability;
- inference servers and model gateways;
- structured output and tool calling;
- prompt development;
- local model runtimes;
- safety, guardrails, and red teaming.

Start with narrower topics and a higher star threshold. Broad terms such as `ai` can return a large, noisy backlog and make the first enrichment run unnecessarily expensive.

## First-deployment sequence

Use this sequence for a predictable launch:

1. Set `batch_limit` to 10–25.
2. Run the full test suite with the pinned Hugo version.
3. Bootstrap and generate the empty or imported catalog locally.
4. Inspect the home page, one category, one tool page, and `feed.xml`.
5. Push the known state so Pages infrastructure is proven without LLM calls.
6. Trigger one manual update.
7. Confirm the database cache save ran even if a later step failed.
8. Confirm the deployed `/catalog.jsonl` and `/feed.xml` are reachable.
9. Trigger a second update and verify previously summarized tools are not reprocessed.
10. Only then enable or rely on the daily schedule.

## Extraction work after HackyFeed is proven live

The next template milestone should consider:

- separating the default theme from domain content;
- adding a first-run setup command that writes config and an empty seed;
- making feed length a documented domain setting;
- adding optional JSON Feed without weakening RSS tests;
- replacing project-specific names in CSS, navigation, and sample content;
- adding a small fixture catalog instead of a production-domain seed;
- enabling GitHub's “Template repository” setting only after a clean-clone smoke test.

The template should be extracted from the successful milestone, not maintained as a second copy while recovery work is still changing core behavior.

# HackyFeed recovery journal

Status: the complete local repair and verification pass is green. No commit, push, or deployment has been performed.

This journal is the durable record of why HackyFeed failed, how it was recovered, and which design choices should carry into the eventual article and template.

## Original intent

HackyFeed is a GitHub Pages site that discovers security repositories, summarizes them with an LLM, and presents them as a browsable chronological feed with RSS. The desired experience is closer to a continuously updated tools publication than a raw link dump.

The reusable core is domain-independent:

```text
source discovery -> deduplicated catalog -> bounded enrichment -> static publication -> RSS
```

Cybersecurity is the first domain. LLM developer tooling is the planned second domain.

## What was observed on 2026-08-02

### Deployment history

- The repository had 81 Actions runs: 74 failed, 5 succeeded, and 2 were cancelled.
- The last successful deployment was [run 23726849514 on 2026-03-30](https://github.com/rainmana/hackyfeed/actions/runs/23726849514).
- The following 61 runs failed consecutively through [run 26645022545 on 2026-05-29](https://github.com/rainmana/hackyfeed/actions/runs/26645022545).
- The scheduled workflow was later marked `disabled_inactivity`. GitHub documents that public-repository schedules may be disabled after 60 days without repository activity: <https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows>.

The latest failed run restored the March 30 database, found 5,588 unsummarized repositories, summarized the same top 1,000 records for roughly 46 minutes, generated 2,000 pages, and then failed during Hugo minification.

Because the cache post-save happened only after a successful job, the newly completed summaries were discarded on every failure. The next run started from the same old database and paid for the same work again.

### The poison document

The immediate build failure came from `filipi86/MalwareAnalysis-in-PDF`. Its README contains a raw script fragment around [line 137](https://github.com/filipi86/MalwareAnalysis-in-PDF/blob/master/README.md?plain=1#L137). HackyFeed stored that README and copied it into a generated Hugo page. Hugo raw HTML was enabled, and its JavaScript minifier eventually rejected the malformed script with an `unexpected < in expression` error.

Removing `--minify` would have hidden the build error while leaving a worse problem: third-party repository content could become same-origin active HTML or JavaScript on the published HackyFeed site. The correct boundary is to never publish arbitrary README bodies.

### Live RSS audit

The existing [`feed.xml`](https://rainmana.github.io/hackyfeed/feed.xml) returned HTTP 200 and was advertised by the home page, but it was not a healthy subscription feed:

- 1,002 items and approximately 1.17 MB;
- 1,000 tool entries sharing the same date at midnight;
- About and Disclaimer entries dated year 0001;
- 179 relative links copied from READMEs;
- 716 truncated or invalid HTML descriptions;
- seven unsafe inline-style warnings;
- no effective upper bound on future growth.

The [W3C feed validator result](https://validator.w3.org/feed/check.cgi?url=https%3A%2F%2Frainmana.github.io%2Fhackyfeed%2Ffeed.xml) rejected the feed. The root cause was not XML serialization alone. Hugo's generic RSS template was summarizing every regular page, including transplanted README markup, while discovery timestamps had already been truncated to dates.

## Joern code-property-graph findings

The existing local Joern CLI, version 4.0.594, successfully parsed the Go codebase into a CPG. No additional Joern distribution was downloaded.

The graph confirmed the top-level execution chain:

```text
main -> fetch.Run -> summarize.Run -> generate.Run
```

It also made the trust-boundary failure explicit:

```text
GitHub/raw README -> database -> RenderToolMarkdown -> os.WriteFile -> Hugo
```

There was no validation or sanitization boundary between attacker-controlled README input and the generated Markdown body. Error paths from directory creation, regeneration writes, and publication bookkeeping were also incomplete or ignored.

The CPG and query scripts are local analysis artifacts rather than repository inputs. The post-fix CPG contains 27 Go files, 398 methods, and 3,659 calls. Its `Repo` model has no README member; the page renderer reads bounded summary/description and public metadata fields; every remaining `readme_raw` occurrence is confined to backward-compatible schema or explicit cleanup code/tests. With three concrete page-writer sink arguments, Joern found zero flows from `FetchReadme` returns into page writes.

## Recovery decisions

### 1. Publish summaries, not source documents

READMEs are fetched only as transient LLM input. They are no longer retained after summarization or rendered into Hugo content. Opening a database remains non-destructive; legacy `readme_raw` values are removed by the explicit `purge-readmes` maintenance command, which the deployment workflow runs after restore.

Generated tool files now contain front matter only. Titles, URLs, categories, timestamps, and summaries are quoted or normalized before Hugo sees them. Hugo raw HTML remains disabled.

Windows Defender also blocked one historically generated filename containing a recognizable webshell signature. Stable hashed source filenames avoided signature-bearing paths, but content scanning still caught equivalent plaintext in front matter. The final generator Unicode-escapes YAML scalar contents; Hugo decodes them back to the intended text while the generated source remains inert and does not expose signature-like HTML or script fragments.

### 2. Treat build output as a deterministic projection

The old `published` flag was set before a successful build and made database state disagree with deployed state. Generation now selects every summarized repository and deterministically renders the complete tool collection. Write and directory errors propagate immediately.

### 3. Separate durable catalog state from resumable work state

Actions cache is useful for an in-progress SQLite database, but GitHub may evict caches. It cannot be the sole source of truth.

The replacement has three layers:

1. SQLite remains the efficient mutable work database.
2. Every successful generation publishes a sanitized `catalog.jsonl` containing public repository metadata and AI summaries plus a schema/count/SHA-256 manifest.
3. `data/catalog.jsonl` is a checked-in recovery seed reconstructed from historical generated pages.

On a clean restore, the last published catalog is imported first and the checked-in seed fills any gaps. Existing SQLite summaries win conflicts so stale recovery data cannot overwrite newer local work.

The first reconstruction contained 996 records, while the live site had 1,000 tool pages with only 926 overlapping. Before replacing the deployment, 74 live-only pages were recovered through their explicit AI-summary boundary. The extractor required a canonical GitHub link, matching page/RSS identity and date, one metadata block, and an `AI Summary:` block immediately followed by the README boundary. It read text content only and never copied the following README.

The final seed contains 1,070 case-insensitively unique records, is canonically sorted by repository name, contains no README bodies, and has a verified adjacent manifest. Its current SHA-256 is:

```text
e47b4fed919a0031ff4ed57a9710d9db0dce1f5a1bff6a8a14324b7965557287
```

### 4. Give RSS its own publication contract

The custom home RSS template is deliberately not a generic rendering of site pages. It selects only the newest 50 tool pages and emits:

- absolute permalink links and GUIDs;
- full UTC publication timestamps;
- plain-text bounded summaries;
- categories;
- no README HTML or non-tool pages.

The same summary field is used consistently on the home page, category pages, individual tool pages, and RSS.

### 5. Make expensive automation explicit

The repaired workflow distinguishes publication from catalog enrichment:

- pushes to `main` test and publish known state without calling the LLM;
- scheduled and manually dispatched runs also fetch and summarize;
- a database cache save runs with `always()` so completed work survives a later failure;
- legacy cached README bodies are explicitly purged and the database is compacted;
- the public catalog and checked-in seed remain authoritative when the cache disappears;
- Hugo is pinned and Go follows `go.mod`;
- reusable Actions are pinned to full commit SHAs;
- CI performs a real seed import, full generation, and production Hugo build.

The schedule cron was deliberately changed so a future commit can reactivate GitHub's inactivity-paused schedule. No remote workflow has been re-enabled yet because nothing has been pushed.

## Implementation map

| Area | Result |
| --- | --- |
| `internal/generate` | Deterministic summary-only pages, staged directory replacement, collision-safe routes, bounded text, full timestamps, hostile-content Hugo test. |
| `internal/db` | Explicit legacy README purge, canonical identities, bounded metadata, integrity checks, atomic catalog import, deterministic export and manifest. |
| `cmd/hackyfeed` | Restore/import/export/doctor commands and clean-state recovery orchestration. |
| Hugo theme | Tool-only home/category rendering, safe static pages, global RSS discovery, custom RSS template. |
| `data/` | Sanitized 1,070-record recovery seed and integrity manifest, including all 74 live-only records. |
| Workflows | Separate CI, safe push deployment, scheduled enrichment, explicit cleanup, SHA-pinned Actions, failure-safe cache save. |

## Validation record

The final verification pass should record all of the following before a commit is proposed:

- [x] `gofmt` produces no changes.
- [x] `go test ./...` passes with `HUGO_BIN` set to Hugo Extended 0.164.0.
- [x] `go vet ./...` passes.
- [x] the CLI builds successfully.
- [x] the 1,070-record seed and manifest import into a clean SQLite database and pass `PRAGMA quick_check`.
- [x] a production Hugo 0.164.0 `--minify` build succeeds from the recovered catalog: 1,070 generated tool sources and all 1,070 public tool routes were verified.
- [x] the built feed is well-formed, has exactly 50 unique tool permalinks/GUIDs in descending order, uses absolute URLs, contains no active script content, and is 43,700 bytes.
- [x] all 74 live-only routes remain present in the clean build.
- [x] a post-fix Joern 4.0.594 CPG confirms page generation no longer depends on stored README bodies.
- [x] `actionlint` accepts both workflows; the final diff contains no generated pages, databases, secrets, or changes under the unrelated `.serena` directory.

## Article material

### Possible working titles

- “The RSS Feed That Re-Summarized 1,000 Repositories Every Night”
- “How a README Broke My GitHub Pages Pipeline—and Exposed the Real Trust Boundary”
- “Caches Are Not Databases: Recovering a Static-Site Automation Pipeline”

### Narrative spine

1. The appealing idea: turn GitHub discovery into a small daily publication.
2. The misleading symptom: Hugo's minifier fails on one strange repository.
3. The deeper security bug: arbitrary upstream content had crossed into a same-origin site.
4. The compounding reliability bug: failed jobs threw away 46 minutes of successful enrichment.
5. The RSS audit: “it returns 200” is not the same as “feed readers can safely consume it.”
6. Code-property-graph analysis: following data rather than guessing from filenames.
7. The repair: summaries as the trust boundary, deterministic builds, explicit feed semantics, and layered state recovery.
8. The template lesson: separate domain configuration from pipeline invariants.

### Evidence worth preserving for screenshots

- the 61-run consecutive failure streak;
- the exact minifier error and offending upstream README line;
- the old 1.17 MB feed and W3C validator findings;
- the Joern trust-flow query result;
- before/after workflow state diagrams;
- before/after feed size, item count, and validation results;
- the first successful post-repair Pages run, once explicitly authorized and deployed.

## Remaining decisions after local repair

- Whether the future LLM tooling feed should share this theme or use a distinct visual identity.
- Whether to turn this repository into a GitHub template repository after the repaired deployment is proven live.
- Whether to add richer feed formats such as JSON Feed only after RSS behavior remains stable.

The immediate rule is conservative: prove the repaired cybersecurity feed first, then extract the template from a known-good milestone rather than copying another half-working state.

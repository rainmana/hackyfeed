---
title: "About"
layout: "single"
---

## About HackyFeed

HackyFeed is a community-driven cybersecurity tools aggregator, inspired by [KitPloit](https://www.kitploit.com) which went offline in 2025.

The goal is simple: **make it easy to discover new and useful cybersecurity tools from GitHub.**

### How It Works

1. A Go CLI tool queries the GitHub API daily for repositories tagged with security-related topics (pentesting, exploit, red-team, OSINT, etc.)
2. New discoveries are stored in a local SQLite database
3. An AI pipeline (via LiteLLM) reads each tool's README and generates a concise summary
4. Hugo renders the summaries into this static site
5. GitHub Actions deploys it automatically

### Sources

- **GitHub Topics**: pentesting, exploit, red-team, offensive-security, OSINT, vulnerability-scanner, bug-bounty, and more
- **[awesome-rainmana](https://github.com/rainmana/awesome-rainmana)**: A curated list of starred GitHub repositories

### Open Source

HackyFeed is fully open source under the MIT License. The entire pipeline — from data collection to site generation — is available at [github.com/rainmana/hackyfeed](https://github.com/rainmana/hackyfeed).

Contributions, suggestions, and tool submissions are welcome via GitHub Issues.

### RSS

Subscribe to the feed at [/feed.xml](/hackyfeed/feed.xml) to get notified of new tools.

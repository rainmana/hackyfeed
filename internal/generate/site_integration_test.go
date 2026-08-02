package generate

import (
	"encoding/xml"
	"fmt"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rainmana/hackyfeed/internal/config"
	"github.com/rainmana/hackyfeed/internal/db"
)

type rssDocument struct {
	Channel struct {
		Items []rssItem `xml:"item"`
	} `xml:"channel"`
}

type rssItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	PubDate     string `xml:"pubDate"`
	Description string `xml:"description"`
	GUID        struct {
		IsPermaLink string `xml:"isPermaLink,attr"`
		Value       string `xml:",chardata"`
	} `xml:"guid"`
}

func TestHugoBuildProducesBoundedToolOnlyRSS(t *testing.T) {
	hugo := os.Getenv("HUGO_BIN")
	if hugo == "" {
		t.Skip("set HUGO_BIN to run the Hugo integration test")
	}

	contentDir := t.TempDir()
	toolsDir := filepath.Join(contentDir, "tools")
	pagesDir := filepath.Join(contentDir, "pages")
	if err := os.MkdirAll(toolsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(pagesDir, 0755); err != nil {
		t.Fatal(err)
	}

	baseTime := time.Date(2026, 3, 21, 12, 34, 0, 0, time.UTC)
	for i := 0; i < 55; i++ {
		summary := fmt.Sprintf("Safe & useful summary %02d <script>not executable</script> %s", i, strings.Repeat("🛠&<>", 200))
		if i == 54 {
			summary = "Use <target> & List<T>; keep <script>not executable</script>."
		}
		repo := db.Repo{
			FullName:  fmt.Sprintf("owner/tool-%02d", i),
			Owner:     "owner",
			Name:      fmt.Sprintf("tool-%02d", i),
			HTMLURL:   fmt.Sprintf("https://github.com/owner/tool-%02d", i),
			AISummary: summary,
			Topics:    "pentesting",
			Source:    "github-topic",
			FirstSeen: baseTime.Add(time.Duration(i) * time.Minute),
		}
		path := filepath.Join(toolsDir, contentFileName(repo.FullName))
		if err := os.WriteFile(path, []byte(RenderToolMarkdown(repo, testCategoriesConfig())), 0644); err != nil {
			t.Fatal(err)
		}
	}

	staticPage := "---\ntitle: \"About test\"\n---\n\nThis page must not enter the tool feed.\n"
	if err := os.WriteFile(filepath.Join(pagesDir, "about.md"), []byte(staticPage), 0644); err != nil {
		t.Fatal(err)
	}

	outputDir := filepath.Join(t.TempDir(), "public")
	cacheDir := filepath.Join(t.TempDir(), "cache")
	siteDir := filepath.Clean(filepath.Join("..", "..", "site"))
	baseConfig, err := filepath.Abs(filepath.Join(siteDir, "hugo.toml"))
	if err != nil {
		t.Fatal(err)
	}
	generatedConfig := filepath.Join(t.TempDir(), "hackyfeed.generated.toml")
	generatedConfigContents, err := renderHugoConfig(&config.SiteConfig{
		Title:       "HackyFeed",
		BaseURL:     "https://rainmana.github.io/hackyfeed/",
		Description: "A cybersecurity tools aggregator",
		Author:      "rainmana",
		Tagline:     "> test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(generatedConfig, generatedConfigContents, 0644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(hugo,
		"--source", siteDir,
		"--config", baseConfig+","+generatedConfig,
		"--contentDir", contentDir,
		"--destination", outputDir,
		"--cacheDir", cacheDir,
		"--cleanDestinationDir",
		"--noBuildLock",
		"--minify",
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("hugo build failed: %v\n%s", err, output)
	}

	feedBytes, err := os.ReadFile(filepath.Join(outputDir, "feed.xml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(feedBytes) > 500_000 {
		t.Fatalf("feed should remain bounded, got %d bytes", len(feedBytes))
	}

	var feed rssDocument
	if err := xml.Unmarshal(feedBytes, &feed); err != nil {
		t.Fatalf("feed is not well-formed XML: %v", err)
	}
	if len(feed.Channel.Items) != 50 {
		t.Fatalf("expected 50 feed items, got %d", len(feed.Channel.Items))
	}
	if feed.Channel.Items[0].Title != "tool-54" {
		t.Fatalf("expected newest tool first, got %q", feed.Channel.Items[0].Title)
	}
	firstDescription := html.UnescapeString(feed.Channel.Items[0].Description)
	if firstDescription != "Use <target> & List<T>; keep <script>not executable</script>." {
		t.Fatalf("RSS summary changed unexpectedly: %q", firstDescription)
	}

	for _, item := range feed.Channel.Items {
		description := html.UnescapeString(item.Description)
		if item.Title == "About test" {
			t.Fatal("non-tool page leaked into RSS")
		}
		if !strings.HasPrefix(item.Link, "https://rainmana.github.io/hackyfeed/tools/") {
			t.Fatalf("expected absolute canonical item link, got %q", item.Link)
		}
		if item.GUID.IsPermaLink != "true" || item.GUID.Value != item.Link {
			t.Fatalf("expected permalink GUID for %q", item.Title)
		}
		if _, err := time.Parse(time.RFC1123Z, item.PubDate); err != nil {
			t.Fatalf("invalid pubDate %q: %v", item.PubDate, err)
		}
		if strings.Contains(strings.ToLower(item.Description), "<script") {
			t.Fatalf("active HTML leaked into RSS description: %q", item.Description)
		}
		if item.Title != "tool-54" && !strings.Contains(description, "Safe & useful") {
			t.Fatalf("RSS description did not round-trip text: %q", description)
		}
		if len(description) > db.MaxSummaryRunes*4 {
			t.Fatalf("RSS description is unexpectedly large: %d", len(description))
		}
	}

	homeBytes, err := os.ReadFile(filepath.Join(outputDir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(homeBytes), "About test") {
		t.Fatal("non-tool page leaked into the home feed")
	}
}

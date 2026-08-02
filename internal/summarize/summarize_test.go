package summarize

import (
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rainmana/hackyfeed/internal/config"
	"github.com/rainmana/hackyfeed/internal/db"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func testResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

func TestParseLLMResponseValidJSON(t *testing.T) {
	input := `{"summary": "A tool for scanning networks."}`
	summary, err := ParseLLMResponse(input)
	if err != nil {
		t.Fatal(err)
	}
	if summary != "A tool for scanning networks." {
		t.Errorf("expected summary, got %q", summary)
	}
}

func TestParseLLMResponsePlainText(t *testing.T) {
	input := "This is a great pentesting tool that does XYZ."
	summary, err := ParseLLMResponse(input)
	if err != nil {
		t.Fatal(err)
	}
	if summary != input {
		t.Errorf("expected raw text as summary, got %q", summary)
	}
}

func TestParseLLMResponseRejectsEmptySummary(t *testing.T) {
	for _, input := range []string{"   \n\t", `{"summary":"  "}`} {
		if _, err := ParseLLMResponse(input); err == nil {
			t.Fatalf("expected empty response %q to fail", input)
		}
	}
}

func TestParseLLMResponseNormalizesWhitespace(t *testing.T) {
	summary, err := ParseLLMResponse("first line\n\nsecond line")
	if err != nil {
		t.Fatal(err)
	}
	if summary != "first line second line" {
		t.Fatalf("unexpected normalized summary %q", summary)
	}
}

func TestResolvePrompt(t *testing.T) {
	got := ResolvePrompt("Write in a {{.Tone}} tone.", "casual")
	if got != "Write in a casual tone." {
		t.Errorf("expected resolved prompt, got %q", got)
	}
}

func TestResolvePromptNoPlaceholder(t *testing.T) {
	tmpl := "Just a plain prompt."
	if ResolvePrompt(tmpl, "technical") != tmpl {
		t.Error("should be unchanged")
	}
}

func TestRunRejectsInvalidReadmeLimit(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "invalid-limit.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := runWithClient(database, LLMConfig{}, &config.SummarizeConfig{Enabled: true, MaxReadmeChars: -1}, http.DefaultClient); err == nil {
		t.Fatal("expected a negative max_readme_chars value to fail")
	}
}

func TestFetchReadmeClassifiesNotFoundAndTransientResponses(t *testing.T) {
	status := http.StatusNotFound
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return testResponse(status, ""), nil
	})}
	if _, err := FetchReadme(client, "owner/tool", 100); !errors.Is(err, ErrReadmeNotFound) {
		t.Fatalf("expected typed not-found error, got %v", err)
	}
	status = http.StatusTooManyRequests
	if _, err := FetchReadme(client, "owner/tool", 100); err == nil || errors.Is(err, ErrReadmeNotFound) {
		t.Fatalf("expected transient 429 error, got %v", err)
	}
	status = http.StatusServiceUnavailable
	if _, err := FetchReadme(client, "owner/tool", 100); err == nil || errors.Is(err, ErrReadmeNotFound) {
		t.Fatalf("expected transient 503 error, got %v", err)
	}
}

func TestFetchReadmeBoundsContentByRunes(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return testResponse(http.StatusOK, strings.Repeat("é", 100)), nil
	})}
	readme, err := FetchReadme(client, "owner/tool", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len([]rune(readme)) != 10 {
		t.Fatalf("expected 10 runes, got %d", len([]rune(readme)))
	}
}

func TestMalformedLLMBaseReturnsErrorInsteadOfPanicking(t *testing.T) {
	_, err := CallLLMWithRetry(http.DefaultClient, LLMConfig{APIBase: "://bad", Model: "test"}, "prompt", "owner/tool", "readme")
	if err == nil {
		t.Fatal("expected malformed LLM API base to fail")
	}
}

func TestTransientReadmeFailureRemainsQueuedAndCanRecover(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "recovery.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.UpsertRepo(database, &db.Repo{FullName: "owner/tool", Description: "fallback", Source: "github-topic"}); err != nil {
		t.Fatal(err)
	}
	readmeAvailable := false
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host == "raw.githubusercontent.com" {
			if !readmeAvailable {
				return testResponse(http.StatusServiceUnavailable, "temporary outage"), nil
			}
			return testResponse(http.StatusOK, "real README"), nil
		}
		return testResponse(http.StatusOK, `{"choices":[{"message":{"role":"assistant","content":"Recovered summary"}}]}`), nil
	})}
	cfg := &config.SummarizeConfig{Enabled: true, MaxReadmeChars: 100, BatchLimit: 10}
	llm := LLMConfig{APIBase: "https://llm.invalid/v1", Model: "test"}
	if err := runWithClient(database, llm, cfg, client); err == nil {
		t.Fatal("expected an all-upstream-failure run to report an error")
	}
	if count, _ := db.SummarizedCount(database); count != 0 {
		t.Fatalf("transient README failure was persisted as a fallback summary")
	}
	readmeAvailable = true
	if err := runWithClient(database, llm, cfg, client); err != nil {
		t.Fatal(err)
	}
	if count, _ := db.SummarizedCount(database); count != 1 {
		t.Fatalf("expected repository to recover on the next run, got %d summaries", count)
	}
}

func TestPerRepositoryErrorsDoNotStarveLaterRows(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "queue.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	for index := 1; index <= 4; index++ {
		if err := db.UpsertRepo(database, &db.Repo{
			FullName: "owner/tool" + string(rune('0'+index)), Stars: 5 - index, Source: "github-topic",
		}); err != nil {
			t.Fatal(err)
		}
	}
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host == "raw.githubusercontent.com" {
			return testResponse(http.StatusOK, "README"), nil
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		if !strings.Contains(string(body), "owner/tool4") {
			return testResponse(http.StatusOK, `{"choices":[{"message":{"role":"assistant","content":"   "}}]}`), nil
		}
		return testResponse(http.StatusOK, `{"choices":[{"message":{"role":"assistant","content":"fourth row succeeded"}}]}`), nil
	})}
	cfg := &config.SummarizeConfig{Enabled: true, MaxReadmeChars: 100, BatchLimit: 10}
	if err := runWithClient(database, LLMConfig{APIBase: "https://llm.invalid/v1", Model: "test"}, cfg, client); err != nil {
		t.Fatal(err)
	}
	if count, _ := db.SummarizedCount(database); count != 1 {
		t.Fatalf("expected the fourth row to be processed, got %d summaries", count)
	}
	if err := runWithClient(database, LLMConfig{APIBase: "https://llm.invalid/v1", Model: "test"}, cfg, client); err != nil {
		t.Fatal(err)
	}
	if count, _ := db.SummarizedCount(database); count != 1 {
		t.Fatalf("persistent bad rows changed completed state, got %d summaries", count)
	}
}

func TestServiceWideLLMFailureTripsCircuit(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "circuit.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	for index := 1; index <= 4; index++ {
		if err := db.UpsertRepo(database, &db.Repo{FullName: "owner/fail" + string(rune('0'+index)), Stars: 5 - index}); err != nil {
			t.Fatal(err)
		}
	}
	llmCalls := 0
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host == "raw.githubusercontent.com" {
			return testResponse(http.StatusOK, "README"), nil
		}
		llmCalls++
		return testResponse(http.StatusUnauthorized, "bad key"), nil
	})}
	err = runWithClient(database, LLMConfig{APIBase: "https://llm.invalid/v1", Model: "test"}, &config.SummarizeConfig{
		Enabled: true, MaxReadmeChars: 100, BatchLimit: 10,
	}, client)
	if err == nil {
		t.Fatal("expected service-wide authentication failures to stop the run")
	}
	if llmCalls != 3 {
		t.Fatalf("expected circuit breaker after 3 calls, got %d", llmCalls)
	}
}

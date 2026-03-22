package summarize

import (
	"testing"
)

func TestParseLLMResponseValidJSON(t *testing.T) {
	input := `{"summary": "A tool for scanning networks.", "install": "pip install netscan"}`
	summary, install, err := ParseLLMResponse(input)
	if err != nil {
		t.Fatal(err)
	}
	if summary != "A tool for scanning networks." {
		t.Errorf("expected summary, got %q", summary)
	}
	if install != "pip install netscan" {
		t.Errorf("expected install, got %q", install)
	}
}

func TestParseLLMResponseMissingInstall(t *testing.T) {
	input := `{"summary": "A great tool.", "install": ""}`
	_, install, err := ParseLLMResponse(input)
	if err != nil {
		t.Fatal(err)
	}
	if install != "See GitHub repository for installation instructions." {
		t.Errorf("expected fallback install, got %q", install)
	}
}

func TestParseLLMResponseInvalidJSON(t *testing.T) {
	// When LLM returns plain text instead of JSON, use it as summary
	input := "This is a great pentesting tool that does XYZ."
	summary, install, err := ParseLLMResponse(input)
	if err != nil {
		t.Fatal(err)
	}
	if summary != input {
		t.Errorf("expected raw text as summary, got %q", summary)
	}
	if install != "See GitHub repository for installation instructions." {
		t.Errorf("expected fallback install, got %q", install)
	}
}

func TestParseLLMResponseWithMarkdownFences(t *testing.T) {
	// LLM sometimes wraps JSON in markdown fences — this should fall back gracefully
	input := "```json\n{\"summary\": \"A tool.\", \"install\": \"go install\"}\n```"
	summary, install, _ := ParseLLMResponse(input)
	// Since the fences make it invalid JSON, it falls back to raw text
	if summary != input {
		t.Errorf("expected raw fallback, got %q", summary)
	}
	if install != "See GitHub repository for installation instructions." {
		t.Errorf("expected fallback install, got %q", install)
	}
}

func TestResolvePrompt(t *testing.T) {
	tmpl := "Write in a {{.Tone}} tone about {{.Tone}} things."
	got := ResolvePrompt(tmpl, "casual")
	expected := "Write in a casual tone about casual things."
	if got != expected {
		t.Errorf("expected %q, got %q", expected, got)
	}
}

func TestResolvePromptNoPlaceholder(t *testing.T) {
	tmpl := "Just a plain prompt with no variables."
	got := ResolvePrompt(tmpl, "technical")
	if got != tmpl {
		t.Errorf("expected unchanged prompt, got %q", got)
	}
}

package summarize

import (
	"testing"
)

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

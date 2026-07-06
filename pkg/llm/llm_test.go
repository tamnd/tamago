package llm

import (
	"encoding/json"
	"testing"
)

func TestExtractJSON(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"bare", `{"a": 1}`, `{"a": 1}`},
		{"fenced", "Here you go:\n```json\n{\"a\": 1}\n```\nDone.", `{"a": 1}`},
		{"prose around", `Sure! The answer is {"a": {"b": 2}} as requested.`, `{"a": {"b": 2}}`},
		{"braces in strings", `{"cmd": "if { x } then }"}`, `{"cmd": "if { x } then }"}`},
		{"escaped quotes", `{"s": "he said \"}\" loudly"}`, `{"s": "he said \"}\" loudly"}`},
	}
	for _, tc := range cases {
		got, err := ExtractJSON(tc.in)
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%s: got %q want %q", tc.name, got, tc.want)
		}
		var v any
		if err := json.Unmarshal([]byte(got), &v); err != nil {
			t.Errorf("%s: extracted JSON does not parse: %v", tc.name, err)
		}
	}
	for _, bad := range []string{"no json here", `{"open": true`} {
		if _, err := ExtractJSON(bad); err == nil {
			t.Errorf("expected an error for %q", bad)
		}
	}
}

func TestProviderSelection(t *testing.T) {
	t.Setenv("TAMAGO_PROVIDER", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	if got := New().Name(); got != "anthropic" {
		t.Errorf("default provider: got %s", got)
	}
	t.Setenv("OPENAI_API_KEY", "sk-test")
	if got := New().Name(); got != "openai" {
		t.Errorf("openai key set: got %s", got)
	}
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test")
	if got := New().Name(); got != "anthropic" {
		t.Errorf("both keys set, anthropic wins: got %s", got)
	}
	t.Setenv("TAMAGO_PROVIDER", "openai")
	if got := New().Name(); got != "openai" {
		t.Errorf("explicit provider wins: got %s", got)
	}
}

func TestOpenAITiers(t *testing.T) {
	t.Setenv("TAMAGO_OPENAI_MODEL", "")
	t.Setenv("TAMAGO_MODEL_FAST", "")
	t.Setenv("TAMAGO_MODEL_STANDARD", "")
	t.Setenv("TAMAGO_MODEL_DEEP", "")
	cl := newOpenAI()
	if cl.ModelForTier("fast") != "gpt-5-mini" || cl.ModelForTier("deep") != "gpt-5" {
		t.Errorf("tier defaults wrong: %v", cl.tiers)
	}
	t.Setenv("TAMAGO_MODEL_DEEP", "gpt-5-codex")
	if newOpenAI().ModelForTier("deep") != "gpt-5-codex" {
		t.Error("TAMAGO_MODEL_DEEP override not honored")
	}
}

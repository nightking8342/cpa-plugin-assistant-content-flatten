package main

import "testing"

func TestParsePluginConfigDefaults(t *testing.T) {
	cfg, err := parsePluginConfig(nil)
	if err != nil {
		t.Fatalf("parsePluginConfig() error = %v", err)
	}
	if got := cfg.separator(); got != "" {
		t.Fatalf("separator() = %q, want empty (CPA-compatible concatenation)", got)
	}
	if got := cfg.protocols(); len(got) != 1 || got[0] != "openai" {
		t.Fatalf("protocols() = %v, want [openai]", got)
	}
	if !cfg.applies("openai", "any-model") {
		t.Fatal("default config should apply to openai targets")
	}
	if cfg.applies("codex", "any-model") {
		t.Fatal("default config must not apply to codex targets")
	}
}

func TestParsePluginConfigIgnoresHostManagedKeys(t *testing.T) {
	// The host injects `enabled` and `priority`; they must not break decoding.
	cfg, err := parsePluginConfig([]byte("enabled: true\npriority: 3\nmodels:\n  - \"gpt-*\"\n"))
	if err != nil {
		t.Fatalf("parsePluginConfig() error = %v", err)
	}
	if len(cfg.Models) != 1 || cfg.Models[0] != "gpt-*" {
		t.Fatalf("models = %v, want [gpt-*]", cfg.Models)
	}
}

func TestParsePluginConfigRejectsBadYAML(t *testing.T) {
	if _, err := parsePluginConfig([]byte("models: [unclosed\n")); err == nil {
		t.Fatal("expected an error for malformed YAML")
	}
}

func TestPluginConfigSeparatorOverride(t *testing.T) {
	cfg, err := parsePluginConfig([]byte("separator: \"\\n\"\n"))
	if err != nil {
		t.Fatalf("parsePluginConfig() error = %v", err)
	}
	if got := cfg.separator(); got != "\n" {
		t.Fatalf("separator() = %q, want a newline", got)
	}
}

func TestPluginConfigApplies(t *testing.T) {
	cases := []struct {
		name     string
		yaml     string
		toFormat string
		model    string
		want     bool
	}{
		{name: "default openai", toFormat: "openai", model: "m", want: true},
		{name: "default non-openai", toFormat: "claude", model: "m", want: false},
		{name: "explicit protocols", yaml: "protocols: [openai, gemini]\n", toFormat: "gemini", model: "m", want: true},
		{name: "allowlist hit", yaml: "models: [\"deepseek-*\"]\n", toFormat: "openai", model: "deepseek-v4.1-flash", want: true},
		{name: "allowlist miss", yaml: "models: [\"gpt-*\"]\n", toFormat: "openai", model: "deepseek-v4.1-flash", want: false},
		{name: "allowlist but empty model", yaml: "models: [\"gpt-*\"]\n", toFormat: "openai", model: "", want: false},
		{name: "denylist wins", yaml: "models: [\"*\"]\nskip_models: [\"deepseek-*\"]\n", toFormat: "openai", model: "deepseek-v4.1-flash", want: false},
		{name: "denylist miss", yaml: "skip_models: [\"gpt-*\"]\n", toFormat: "openai", model: "deepseek-v4.1-flash", want: true},
		{name: "case insensitive model", yaml: "models: [\"DEEPSEEK-*\"]\n", toFormat: "openai", model: "deepseek-v4.1-flash", want: true},
		{name: "case insensitive protocol", yaml: "protocols: [\"OpenAI\"]\n", toFormat: "openai", model: "m", want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := parsePluginConfig([]byte(tc.yaml))
			if err != nil {
				t.Fatalf("parsePluginConfig() error = %v", err)
			}
			if got := cfg.applies(tc.toFormat, tc.model); got != tc.want {
				t.Fatalf("applies(%q, %q) = %v, want %v", tc.toFormat, tc.model, got, tc.want)
			}
		})
	}
}

func TestWildcardMatch(t *testing.T) {
	cases := []struct {
		pattern string
		value   string
		want    bool
	}{
		{"*", "anything", true},
		{"exact", "exact", true},
		{"exact", "exactly", false},
		{"deepseek-*", "deepseek-v4.1-flash", true},
		{"deepseek-*", "glm-5", false},
		{"*-flash", "deepseek-v4.1-flash", true},
		{"*-flash", "deepseek-v4.1", false},
		{"*v4*flash*", "deepseek-v4.1-flash", true},
		{"*v9*flash*", "deepseek-v4.1-flash", false},
		{"", "anything", false},
	}
	for _, tc := range cases {
		if got := wildcardMatch(tc.pattern, tc.value); got != tc.want {
			t.Fatalf("wildcardMatch(%q, %q) = %v, want %v", tc.pattern, tc.value, got, tc.want)
		}
	}
}

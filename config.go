package main

import (
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	// defaultSeparator joins multiple text parts of one assistant message.
	//
	// Empty means direct concatenation, which is exactly what CPA's own Gemini
	// (`onlyTextContent`) and Interactions (`textOnly`) translators do when they
	// flatten text-only content. Matching them keeps this plugin's output
	// byte-identical to the paths CPA already handles itself.
	//
	// Caveat: a Claude assistant turn can carry two text blocks around a tool
	// call ("pre" / "post"), and concatenating those yields "prepost". Set
	// `separator: "\n"` in the plugin config if that ever shows up.
	defaultSeparator = ""

	// defaultProtocol is the only target format whose assistant content has to
	// be a string. Other formats either require content arrays (OpenAI
	// Responses / Codex) or are not OpenAI-shaped at all.
	defaultProtocol = "openai"
)

// pluginConfig is the plugin-owned configuration read from
// `plugins.configs.assistant-content-flatten` in CPA's config.yaml.
//
// Host-managed keys (`enabled`, `priority`) and unknown keys are ignored.
type pluginConfig struct {
	// Models restricts the rewrite to matching model names. Supports "*"
	// wildcards. Empty means "all models".
	Models []string `yaml:"models"`
	// SkipModels excludes matching model names. Supports "*" wildcards and
	// takes precedence over Models.
	SkipModels []string `yaml:"skip_models"`
	// Protocols restricts the rewrite to specific *target* formats
	// (`to_format`). Defaults to ["openai"].
	Protocols []string `yaml:"protocols"`
	// Separator joins multiple text parts. Defaults to "\n"; "" concatenates.
	Separator *string `yaml:"separator"`
	// Debug logs every rewrite to stderr.
	Debug bool `yaml:"debug"`
}

// parsePluginConfig decodes the YAML block the host passes on
// plugin.register / plugin.reconfigure. An empty block yields defaults.
func parsePluginConfig(raw []byte) (pluginConfig, error) {
	cfg := pluginConfig{}
	if len(raw) == 0 {
		return cfg, nil
	}
	if errUnmarshal := yaml.Unmarshal(raw, &cfg); errUnmarshal != nil {
		return pluginConfig{}, errUnmarshal
	}
	return cfg, nil
}

// separator returns the configured join string, defaulting to "\n".
func (c pluginConfig) separator() string {
	if c.Separator == nil {
		return defaultSeparator
	}
	return *c.Separator
}

// protocols returns the configured target formats, defaulting to ["openai"].
func (c pluginConfig) protocols() []string {
	if len(c.Protocols) == 0 {
		return []string{defaultProtocol}
	}
	return c.Protocols
}

// applies reports whether the rewrite should run for this request.
func (c pluginConfig) applies(toFormat, model string) bool {
	if !matchesAnyProtocol(c.protocols(), toFormat) {
		return false
	}

	model = strings.ToLower(strings.TrimSpace(model))
	for _, pattern := range c.SkipModels {
		if wildcardMatch(strings.ToLower(strings.TrimSpace(pattern)), model) {
			return false
		}
	}

	if len(c.Models) == 0 {
		return true
	}
	if model == "" {
		// An allowlist is configured but the request carries no model name, so
		// it cannot be matched. Leave the payload alone.
		return false
	}
	for _, pattern := range c.Models {
		if wildcardMatch(strings.ToLower(strings.TrimSpace(pattern)), model) {
			return true
		}
	}
	return false
}

func matchesAnyProtocol(protocols []string, toFormat string) bool {
	toFormat = strings.ToLower(strings.TrimSpace(toFormat))
	for _, protocol := range protocols {
		protocol = strings.ToLower(strings.TrimSpace(protocol))
		if protocol == "" {
			continue
		}
		if protocol == toFormat {
			return true
		}
	}
	return false
}

// wildcardMatch reports whether value matches a pattern that may contain "*".
// Matching is case-insensitive on already-lowercased input.
func wildcardMatch(pattern, value string) bool {
	if pattern == "" {
		return false
	}
	if pattern == "*" {
		return true
	}
	if !strings.Contains(pattern, "*") {
		return pattern == value
	}

	segments := strings.Split(pattern, "*")

	if head := segments[0]; head != "" {
		if !strings.HasPrefix(value, head) {
			return false
		}
		value = value[len(head):]
	}
	if tail := segments[len(segments)-1]; tail != "" {
		if !strings.HasSuffix(value, tail) {
			return false
		}
		value = value[:len(value)-len(tail)]
	}
	for _, segment := range segments[1 : len(segments)-1] {
		if segment == "" {
			continue
		}
		index := strings.Index(value, segment)
		if index < 0 {
			return false
		}
		value = value[index+len(segment):]
	}
	return true
}

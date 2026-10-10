package main

import (
	"encoding/json"
	"testing"

	"github.com/tidwall/gjson"
)

// claudeToOpenAIPayload is the shape CPA emits for a Claude Code request routed
// to an OpenAI-compatible upstream: the assistant turn that carries visible
// text has `content` as an array of content parts, which strict upstreams drop.
const claudeToOpenAIPayload = `{
  "model": "deepseek-v4.1-flash",
  "messages": [
    {"role": "system", "content": [{"type": "text", "text": "system prompt"}]},
    {"role": "user", "content": [{"type": "text", "text": "hello"}]},
    {"role": "assistant", "content": "", "tool_calls": [{"id": "call_1", "type": "function", "function": {"name": "Glob", "arguments": "{}"}}]},
    {"role": "tool", "tool_call_id": "call_1", "content": "file list"},
    {"role": "assistant", "content": [{"type": "text", "text": "三次工具调用已完成。"}], "reasoning_content": "thinking text"},
    {"role": "user", "content": [{"type": "text", "text": "repeat that"}]}
  ]
}`

func TestFlattenAssistantContentRewritesTextOnlyAssistantMessage(t *testing.T) {
	out, stats, err := flattenAssistantContent([]byte(claudeToOpenAIPayload), defaultSeparator)
	if err != nil {
		t.Fatalf("flattenAssistantContent() error = %v", err)
	}
	if stats.Flattened != 1 {
		t.Fatalf("flattened = %d, want 1", stats.Flattened)
	}

	assistant := gjson.GetBytes(out, "messages.4")
	if got := assistant.Get("content").Type; got != gjson.String {
		t.Fatalf("messages.4.content type = %v, want String", got)
	}
	if got, want := assistant.Get("content").String(), "三次工具调用已完成。"; got != want {
		t.Fatalf("messages.4.content = %q, want %q", got, want)
	}
	// Untouched neighbours.
	if got := gjson.GetBytes(out, "messages.5.content").Type; got != gjson.JSON {
		t.Fatalf("user message content type = %v, want JSON (arrays must stay arrays)", got)
	}
	if got, want := assistant.Get("reasoning_content").String(), "thinking text"; got != want {
		t.Fatalf("reasoning_content = %q, want %q", got, want)
	}
	if !gjson.GetBytes(out, "messages.2.tool_calls").Exists() {
		t.Fatal("tool_calls on the tool-call turn were dropped")
	}
	if got, want := gjson.GetBytes(out, "messages.2.content").String(), ""; got != want {
		t.Fatalf("messages.2.content = %q, want %q", got, want)
	}
}

func TestFlattenAssistantContentKeepsMultimodalArrays(t *testing.T) {
	// Regression guard for the failure mode that got the upstream PR rejected:
	// flattening must never drop image / file / audio parts.
	cases := map[string]string{
		"image_url":    `{"role":"assistant","content":[{"type":"text","text":"look"},{"type":"image_url","image_url":{"url":"data:image/png;base64,AA"}}]}`,
		"input_audio":  `{"role":"assistant","content":[{"type":"input_audio","input_audio":{"data":"AA","format":"wav"}}]}`,
		"file":         `{"role":"assistant","content":[{"type":"text","text":"doc"},{"type":"file","file":{"file_id":"f1"}}]}`,
		"output_text":  `{"role":"assistant","content":[{"type":"output_text","text":"responses shape"}]}`,
		"refusal":      `{"role":"assistant","content":[{"type":"refusal","refusal":"no"}]}`,
		"text_no_body": `{"role":"assistant","content":[{"type":"text"}]}`,
		// CPA hoists a message-level cache_control onto the last content part;
		// flattening it would silently drop the cache breakpoint.
		"cache_control": `{"role":"assistant","content":[{"type":"text","text":"cached","cache_control":{"type":"ephemeral"}}]}`,
		"extra_key":     `{"role":"assistant","content":[{"type":"text","text":"x","index":0}]}`,
	}
	for name, message := range cases {
		t.Run(name, func(t *testing.T) {
			payload := `{"messages":[` + message + `]}`
			out, stats, err := flattenAssistantContent([]byte(payload), defaultSeparator)
			if err != nil {
				t.Fatalf("flattenAssistantContent() error = %v", err)
			}
			if stats.Flattened != 0 {
				t.Fatalf("flattened = %d, want 0", stats.Flattened)
			}
			if string(out) != payload {
				t.Fatalf("payload changed:\n got %s\nwant %s", out, payload)
			}
		})
	}
}

func TestFlattenAssistantContentJoinsMultipleTextParts(t *testing.T) {
	payload := `{"messages":[{"role":"assistant","content":[{"type":"text","text":"pre"},{"type":"text","text":"post"}],"tool_calls":[{"id":"c","type":"function","function":{"name":"f","arguments":"{}"}}]}]}`

	// Default separator is "" so the output matches CPA's own Gemini /
	// Interactions flattening byte for byte.
	out, stats, err := flattenAssistantContent([]byte(payload), defaultSeparator)
	if err != nil {
		t.Fatalf("flattenAssistantContent() error = %v", err)
	}
	if stats.Flattened != 1 {
		t.Fatalf("flattened = %d, want 1", stats.Flattened)
	}
	if got, want := gjson.GetBytes(out, "messages.0.content").String(), "prepost"; got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
	if !gjson.GetBytes(out, "messages.0.tool_calls").Exists() {
		t.Fatal("tool_calls were dropped while flattening text")
	}

	// Opt-in newline separator for turns whose text blocks must stay apart.
	if err != nil {
		t.Fatalf("flattenAssistantContent() error = %v", err)
	}
	newlined, _, err := flattenAssistantContent([]byte(payload), "\n")
	if err != nil {
		t.Fatalf("flattenAssistantContent() error = %v", err)
	}
	if got, want := gjson.GetBytes(newlined, "messages.0.content").String(), "pre\npost"; got != want {
		t.Fatalf("content with newline separator = %q, want %q", got, want)
	}
}

func TestFlattenAssistantContentAcceptsBareStringParts(t *testing.T) {
	payload := `{"messages":[{"role":"assistant","content":["a","b"]}]}`
	out, stats, err := flattenAssistantContent([]byte(payload), defaultSeparator)
	if err != nil {
		t.Fatalf("flattenAssistantContent() error = %v", err)
	}
	if stats.Flattened != 1 {
		t.Fatalf("flattened = %d, want 1", stats.Flattened)
	}
	if got, want := gjson.GetBytes(out, "messages.0.content").String(), "ab"; got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
}

func TestFlattenAssistantContentLeavesEverythingElseAlone(t *testing.T) {
	cases := map[string]string{
		"content already a string":  `{"messages":[{"role":"assistant","content":"plain"}]}`,
		"content is null":           `{"messages":[{"role":"assistant","content":null}]}`,
		"empty content array":       `{"messages":[{"role":"assistant","content":[]}]}`,
		"user array content":        `{"messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}]}`,
		"system array content":      `{"messages":[{"role":"system","content":[{"type":"text","text":"sys"}]}]}`,
		"no messages key":           `{"model":"m"}`,
		"messages not an array":     `{"messages":{"role":"assistant"}}`,
		"not json":                  `not json at all`,
		"assistant content missing": `{"messages":[{"role":"assistant"}]}`,
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			out, stats, err := flattenAssistantContent([]byte(payload), defaultSeparator)
			if err != nil {
				t.Fatalf("flattenAssistantContent() error = %v", err)
			}
			if stats.Flattened != 0 {
				t.Fatalf("flattened = %d, want 0", stats.Flattened)
			}
			if string(out) != payload {
				t.Fatalf("payload changed:\n got %s\nwant %s", out, payload)
			}
		})
	}
}

func TestFlattenAssistantContentRewritesEveryEligibleMessage(t *testing.T) {
	payload := `{"messages":[
		{"role":"assistant","content":[{"type":"text","text":"one"}]},
		{"role":"user","content":[{"type":"text","text":"q"}]},
		{"role":"assistant","content":[{"type":"text","text":"two"},{"type":"image_url","image_url":{"url":"u"}}]},
		{"role":"assistant","content":[{"type":"text","text":"three"}]}
	]}`

	out, stats, err := flattenAssistantContent([]byte(payload), defaultSeparator)
	if err != nil {
		t.Fatalf("flattenAssistantContent() error = %v", err)
	}
	if stats.Scanned != 4 {
		t.Fatalf("scanned = %d, want 4", stats.Scanned)
	}
	if stats.Flattened != 2 {
		t.Fatalf("flattened = %d, want 2", stats.Flattened)
	}

	var decoded struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if errUnmarshal := json.Unmarshal(out, &decoded); errUnmarshal != nil {
		t.Fatalf("result is not valid JSON: %v", errUnmarshal)
	}
	for index, want := range []bool{true, false, false, true} {
		isString := len(decoded.Messages[index].Content) > 0 && decoded.Messages[index].Content[0] == '"'
		if isString != want {
			t.Fatalf("messages.%d isString = %v, want %v", index, isString, want)
		}
	}
}

package main

import (
	"strconv"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	roleAssistant = "assistant"
	partTypeText  = "text"
)

// flattenStats reports what one pass over a payload did.
type flattenStats struct {
	// Scanned counts the messages inspected.
	Scanned int
	// Flattened counts the assistant messages rewritten from a text-only
	// content array into a plain string.
	Flattened int
}

// flattenAssistantContent rewrites assistant messages whose `content` is a
// text-only array of content parts into a plain JSON string.
//
// Background: CLIProxyAPI's "-> OpenAI chat completions" translators emit
// assistant content as an array of content parts. That is valid per the OpenAI
// schema (assistant content may be a string or an array of text parts), but a
// whole class of OpenAI-compatible upstreams — vLLM / NIM style servers whose
// chat template assumes `content` is always a string — silently drop such a
// message instead of rejecting the request. The model then sees a conversation
// turn with no visible text and reports that its own previous reply is missing.
//
// Two of CPA's own translators already avoid this by flattening text-only
// content before writing the payload:
//
//	openai/gemini/openai_gemini_request.go        (onlyTextContent)
//	openai/interactions/.../interactions_openai_request.go  (textOnly)
//
// while two others do not:
//
//	openai/claude/openai_claude_request.go
//	openai/openai/responses/openai_openai-responses_request.go
//
// This plugin applies the same rule to the payload after translation, so it
// covers both missing paths without patching the proxy.
//
// The rule deliberately mirrors CPA's own behavior:
//
//   - only messages with role == "assistant";
//   - only when the content array is non-empty and *every* part is a text part;
//   - any non-text part (image_url, file, input_audio, video_url, ...) leaves
//     the array untouched, so multimodal payloads keep their semantics;
//   - `reasoning_content` and `tool_calls` are never touched.
//
// separator is inserted between text parts when a message has more than one.
// The returned payload is the original slice when nothing changed.
func flattenAssistantContent(payload []byte, separator string) ([]byte, flattenStats, error) {
	var stats flattenStats

	messages := gjson.GetBytes(payload, "messages")
	if !messages.Exists() || !messages.IsArray() {
		return payload, stats, nil
	}

	out := payload
	index := -1
	for _, message := range messages.Array() {
		index++
		stats.Scanned++

		if message.Get("role").String() != roleAssistant {
			continue
		}
		content := message.Get("content")
		if !content.IsArray() {
			continue
		}
		parts := content.Array()
		if len(parts) == 0 {
			continue
		}

		texts := make([]string, 0, len(parts))
		textOnly := true
		for _, part := range parts {
			text, ok := assistantTextPart(part)
			if !ok {
				textOnly = false
				break
			}
			texts = append(texts, text)
		}
		if !textOnly {
			continue
		}

		path := "messages." + strconv.Itoa(index) + ".content"
		updated, errSet := sjson.SetBytes(out, path, strings.Join(texts, separator))
		if errSet != nil {
			return payload, stats, errSet
		}
		out = updated
		stats.Flattened++
	}

	return out, stats, nil
}

// assistantTextPart reports whether a content part carries assistant-visible
// text, and returns that text.
//
// Only the OpenAI chat-completions text part shape produced by CPA is accepted:
// an object with no `type` or `type == "text"` and a string `text` field. Bare
// JSON strings inside the parts array are accepted too, because some clients
// send them.
//
// Everything else — `image_url`, `file`, `input_audio`, `video_url`, `refusal`,
// and notably `output_text` (a Responses-format part, where content arrays are
// the required shape) — is treated as non-text so the array is left alone.
func assistantTextPart(part gjson.Result) (string, bool) {
	switch part.Type {
	case gjson.String:
		return part.String(), true
	case gjson.JSON:
		if !part.IsObject() {
			return "", false
		}
		switch part.Get("type").String() {
		case "", partTypeText:
			text := part.Get("text")
			if text.Type != gjson.String {
				return "", false
			}
			return text.String(), true
		}
	}
	return "", false
}

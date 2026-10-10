package main

import (
	"encoding/json"
	"testing"

	"github.com/tidwall/gjson"
)

func decodeEnvelope(t *testing.T, raw []byte) envelope {
	t.Helper()
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("decode envelope: %v (raw=%s)", err, raw)
	}
	return env
}

func registerPlugin(t *testing.T, configYAML string) *plugin {
	t.Helper()
	p := newPlugin()
	request, err := json.Marshal(pluginConfigRequest{ConfigYAML: []byte(configYAML), SchemaVersion: 6})
	if err != nil {
		t.Fatalf("marshal register request: %v", err)
	}
	raw, errHandle := p.handleMethod(methodPluginRegister, request)
	if errHandle != nil {
		t.Fatalf("handleMethod(plugin.register) error = %v", errHandle)
	}
	env := decodeEnvelope(t, raw)
	if !env.OK {
		t.Fatalf("register failed: %s", env.Error.Message)
	}
	if got := gjson.GetBytes(env.Result, "schema_version").Uint(); got != uint64(schemaVersion) {
		t.Fatalf("schema_version = %d, want %d", got, schemaVersion)
	}
	if !gjson.GetBytes(env.Result, "capabilities.request_normalizer").Bool() {
		t.Fatal("plugin did not advertise request_normalizer")
	}
	if got, want := gjson.GetBytes(env.Result, "metadata.Name").String(), pluginName; got != want {
		t.Fatalf("metadata.Name = %q, want %q", got, want)
	}
	return p
}

func callNormalize(t *testing.T, p *plugin, toFormat, model string, body string) []byte {
	t.Helper()
	request, err := json.Marshal(requestTransformRequest{
		FromFormat: "claude",
		ToFormat:   toFormat,
		Model:      model,
		Stream:     true,
		Body:       []byte(body),
	})
	if err != nil {
		t.Fatalf("marshal normalize request: %v", err)
	}
	raw, errHandle := p.handleMethod(methodRequestNormalize, request)
	if errHandle != nil {
		t.Fatalf("handleMethod(request.normalize) error = %v", errHandle)
	}
	env := decodeEnvelope(t, raw)
	if !env.OK {
		t.Fatalf("normalize failed: %s", env.Error.Message)
	}
	var response payloadResponse
	if errUnmarshal := json.Unmarshal(env.Result, &response); errUnmarshal != nil {
		t.Fatalf("decode payload response: %v", errUnmarshal)
	}
	return response.Body
}

func TestRegisterAdvertisesRequestNormalizer(t *testing.T) {
	registerPlugin(t, "")
}

func TestNormalizeRoundTripFlattensAssistantContent(t *testing.T) {
	p := registerPlugin(t, "")

	body := callNormalize(t, p, "openai", "deepseek-v4.1-flash", claudeToOpenAIPayload)
	if got := gjson.GetBytes(body, "messages.4.content").Type; got != gjson.String {
		t.Fatalf("messages.4.content type = %v, want String", got)
	}
	if got, want := gjson.GetBytes(body, "messages.4.content").String(), "三次工具调用已完成。"; got != want {
		t.Fatalf("messages.4.content = %q, want %q", got, want)
	}
}

func TestNormalizeRespectsProtocolGate(t *testing.T) {
	p := registerPlugin(t, "")

	body := callNormalize(t, p, "codex", "gpt-5.5", claudeToOpenAIPayload)
	if string(body) != claudeToOpenAIPayload {
		t.Fatal("payload must pass through untouched for non-openai targets")
	}
}

func TestNormalizeRespectsModelGates(t *testing.T) {
	p := registerPlugin(t, "models: [\"gpt-*\"]\n")
	if body := callNormalize(t, p, "openai", "deepseek-v4.1-flash", claudeToOpenAIPayload); string(body) != claudeToOpenAIPayload {
		t.Fatal("model not in allowlist must pass through untouched")
	}
	if body := callNormalize(t, p, "openai", "gpt-5.5", claudeToOpenAIPayload); gjson.GetBytes(body, "messages.4.content").Type != gjson.String {
		t.Fatal("model in allowlist must be rewritten")
	}

	p = registerPlugin(t, "skip_models: [\"deepseek-*\"]\n")
	if body := callNormalize(t, p, "openai", "deepseek-v4.1-flash", claudeToOpenAIPayload); string(body) != claudeToOpenAIPayload {
		t.Fatal("denylisted model must pass through untouched")
	}
}

func TestNormalizeHonoursReconfigure(t *testing.T) {
	p := registerPlugin(t, "models: [\"gpt-*\"]\n")
	if body := callNormalize(t, p, "openai", "deepseek-v4.1-flash", claudeToOpenAIPayload); string(body) != claudeToOpenAIPayload {
		t.Fatal("model should be filtered out before reconfiguration")
	}

	request, err := json.Marshal(pluginConfigRequest{ConfigYAML: []byte(""), SchemaVersion: 6})
	if err != nil {
		t.Fatalf("marshal reconfigure request: %v", err)
	}
	raw, errHandle := p.handleMethod(methodPluginReconfigure, request)
	if errHandle != nil {
		t.Fatalf("handleMethod(plugin.reconfigure) error = %v", errHandle)
	}
	if env := decodeEnvelope(t, raw); !env.OK {
		t.Fatalf("reconfigure failed: %s", env.Error.Message)
	}

	if body := callNormalize(t, p, "openai", "deepseek-v4.1-flash", claudeToOpenAIPayload); gjson.GetBytes(body, "messages.4.content").Type != gjson.String {
		t.Fatal("configuration was not reloaded")
	}
}

func TestUnknownMethodReturnsEnvelopeError(t *testing.T) {
	raw, err := newPlugin().handleMethod("nope.method", nil)
	if err != nil {
		t.Fatalf("handleMethod() error = %v, want nil", err)
	}
	env := decodeEnvelope(t, raw)
	if env.OK {
		t.Fatal("unknown method must not report success")
	}
	if got, want := env.Error.Code, "unknown_method"; got != want {
		t.Fatalf("error code = %q, want %q", got, want)
	}
}

func TestNormalizeSurvivesMalformedBody(t *testing.T) {
	p := registerPlugin(t, "")
	if body := callNormalize(t, p, "openai", "m", "not json"); string(body) != "not json" {
		t.Fatalf("body = %q, want the original payload", body)
	}
}

func TestNormalizeHandlesEmptyBody(t *testing.T) {
	p := registerPlugin(t, "")
	if body := callNormalize(t, p, "openai", "m", ""); len(body) != 0 {
		t.Fatalf("body = %q, want empty", body)
	}
}

package main

import (
	"encoding/json"
	"reflect"
	"sort"
	"testing"
)

// These tests pin the JSON wire contract against CLIProxyAPI's plugin ABI. If a
// key name drifts, the plugin silently stops working, so the names are asserted
// rather than assumed.

func jsonKeys(t *testing.T, value any) []string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]any
	if errUnmarshal := json.Unmarshal(raw, &decoded); errUnmarshal != nil {
		t.Fatalf("unmarshal: %v", errUnmarshal)
	}
	keys := make([]string, 0, len(decoded))
	for key := range decoded {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func assertKeys(t *testing.T, value any, want ...string) {
	t.Helper()
	sort.Strings(want)
	if got := jsonKeys(t, value); !reflect.DeepEqual(got, want) {
		t.Fatalf("json keys = %v, want %v", got, want)
	}
}

func TestLifecycleRequestKeys(t *testing.T) {
	// Mirrors internal/pluginhost/rpcLifecycleRequest.
	assertKeys(t, pluginConfigRequest{}, "config_yaml", "schema_version")
}

func TestRegistrationKeys(t *testing.T) {
	// Mirrors internal/pluginhost/rpcRegistration + rpcCapabilities.
	assertKeys(t, registration{}, "capabilities", "metadata", "schema_version")
	assertKeys(t, registrationCapabilities{}, "request_normalizer")
}

func TestMetadataKeys(t *testing.T) {
	// Mirrors pluginapi.Metadata, which carries no json tags: the Go field names
	// are the wire keys.
	assertKeys(t, metadata{}, "Author", "ConfigFields", "GitHubRepository", "Logo", "Name", "Version")
	assertKeys(t, configField{}, "Description", "EnumValues", "Name", "Type")
}

func TestTransformRequestKeys(t *testing.T) {
	// Mirrors pluginapi.RequestTransformRequest.
	assertKeys(t, requestTransformRequest{}, "Body", "FromFormat", "Model", "Stream", "ToFormat")
	// Mirrors pluginapi.PayloadResponse.
	assertKeys(t, payloadResponse{}, "Body")
}

func TestEnvelopeKeys(t *testing.T) {
	// Mirrors pluginabi.Envelope.
	assertKeys(t, envelope{OK: true, Result: json.RawMessage(`{}`)}, "ok", "result")
	assertKeys(t, envelope{OK: false, Error: &envelopeError{}}, "error", "ok")
}

func TestBodyIsBase64OnTheWire(t *testing.T) {
	// Both sides use []byte, so encoding/json base64-encodes it symmetrically.
	// This guards against someone "helpfully" switching Body to a string.
	raw, err := json.Marshal(requestTransformRequest{Body: []byte(`{"a":1}`)})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if got, want := string(raw), `{"FromFormat":"","ToFormat":"","Model":"","Stream":false,"Body":"eyJhIjoxfQ=="}`; got != want {
		t.Fatalf("wire payload = %s, want %s", got, want)
	}
}

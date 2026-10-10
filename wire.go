package main

// This file mirrors the JSON wire contract CLIProxyAPI exchanges with
// dynamic-library plugins. It is re-declared here instead of importing the
// CLIProxyAPI module on purpose:
//
//   - the contract is plain JSON, so a local copy is wire-compatible;
//   - the upstream module currently requires a newer Go toolchain and a large
//     module graph, which a ~300 line plugin does not need;
//   - the plugin stays buildable with a stock toolchain and no replace
//     directive.
//
// Field names matter. The upstream structs (sdk/pluginapi) carry no `json`
// tags, so their Go field names ARE the wire keys; the lifecycle/registration
// envelopes (internal/pluginhost/rpc_schema.go) use snake_case tags. wire_test.go
// pins both.
//
// Source of truth at the time of writing: CLIProxyAPI v8.0.23
// (commit d318bcc3).

const (
	// abiVersion tracks the native C ABI shape. Mirrors pluginabi.ABIVersion.
	abiVersion uint32 = 1

	// schemaVersion is the JSON contract this plugin advertises.
	//
	// The host rejects a plugin that claims a version newer than its own, and
	// treats 0 as the original contract. `request.normalize` — the only
	// capability implemented here — has kept the same payload shape across every
	// schema version (the versioned changes concern stream-chunk interceptor
	// payloads and management-response escaping). Advertising the oldest
	// contract therefore keeps this plugin loadable on both older and newer
	// hosts, and is an honest description of what it relies on.
	schemaVersion uint32 = 1
)

// Method names exchanged over the plugin ABI. Mirrors pluginabi method constants.
const (
	methodPluginRegister    = "plugin.register"
	methodPluginReconfigure = "plugin.reconfigure"
	methodRequestNormalize  = "request.normalize"
)

// pluginConfigRequest is the lifecycle payload the host sends on
// plugin.register and plugin.reconfigure.
//
// Mirrors internal/pluginhost/rpcLifecycleRequest.
type pluginConfigRequest struct {
	ConfigYAML    []byte `json:"config_yaml"`
	SchemaVersion uint32 `json:"schema_version"`
}

// registration is the plugin.register reply.
//
// Mirrors internal/pluginhost/rpcRegistration.
type registration struct {
	SchemaVersion uint32                   `json:"schema_version"`
	Metadata      metadata                 `json:"metadata"`
	Capabilities  registrationCapabilities `json:"capabilities"`
}

// metadata describes the plugin to the management UI.
//
// Mirrors pluginapi.Metadata, which has no json tags (Go field names are the
// wire keys).
type metadata struct {
	Name             string        `json:"Name"`
	Version          string        `json:"Version"`
	Author           string        `json:"Author"`
	GitHubRepository string        `json:"GitHubRepository"`
	Logo             string        `json:"Logo"`
	ConfigFields     []configField `json:"ConfigFields"`
}

// configField describes one plugin-owned configuration key.
//
// Mirrors pluginapi.ConfigField (no json tags upstream).
type configField struct {
	Name        string   `json:"Name"`
	Type        string   `json:"Type"`
	EnumValues  []string `json:"EnumValues"`
	Description string   `json:"Description"`
}

// Config field types understood by management clients. Mirrors pluginapi.ConfigFieldType.
const (
	configFieldTypeString  = "string"
	configFieldTypeBoolean = "boolean"
	configFieldTypeArray   = "array"
)

// registrationCapabilities advertises which host integration interfaces the
// plugin implements. Only request_normalizer is set.
//
// Mirrors the subset of internal/pluginhost/rpcCapabilities that matters here.
type registrationCapabilities struct {
	RequestNormalizer bool `json:"request_normalizer"`
}

// requestTransformRequest is the request.normalize payload.
//
// Mirrors pluginapi.RequestTransformRequest (no json tags upstream). Body is a
// byte slice on both sides, so encoding/json base64-encodes it symmetrically.
type requestTransformRequest struct {
	FromFormat string
	ToFormat   string
	Model      string
	Stream     bool
	Body       []byte
}

// payloadResponse is the request.normalize reply body.
//
// Mirrors pluginapi.PayloadResponse (no json tags upstream).
type payloadResponse struct {
	Body []byte
}

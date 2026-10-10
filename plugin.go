package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sync/atomic"
)

// Plugin identity shown in CPA's management UI. Change these before publishing.
const (
	pluginID      = "assistant-content-flatten"
	pluginName    = "Assistant Content Flatten"
	pluginVersion = "0.1.0"
	pluginAuthor  = "nightking8342"
	pluginRepoURL = ""
)

// envelope is the response wrapper every ABI method returns.
// Mirrors pluginabi.Envelope.
type envelope struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *envelopeError  `json:"error,omitempty"`
}

// envelopeError is the failure shape inside envelope.
// Mirrors pluginabi.Error.
type envelopeError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// pluginInstance is the single instance the C ABI shim talks to.
var pluginInstance = newPlugin()

// plugin holds the live configuration. The host may call
// plugin.reconfigure at any time while requests are in flight, so the config is
// swapped atomically rather than mutated in place.
type plugin struct {
	config atomic.Value // holds pluginConfig
}

func newPlugin() *plugin {
	p := &plugin{}
	p.config.Store(pluginConfig{})
	return p
}

func (p *plugin) currentConfig() pluginConfig {
	cfg, _ := p.config.Load().(pluginConfig)
	return cfg
}

// handleMethod dispatches one ABI call. Returning a non-nil error makes the
// host treat the call as failed; method-level failures are reported inside the
// envelope instead so the host logs them without fusing the plugin.
func (p *plugin) handleMethod(method string, request []byte) ([]byte, error) {
	switch method {
	case methodPluginRegister, methodPluginReconfigure:
		if errConfigure := p.configure(request); errConfigure != nil {
			return nil, errConfigure
		}
		return okEnvelope(pluginRegistration())
	case methodRequestNormalize:
		return p.normalize(request)
	default:
		// Unknown methods are answered rather than failed, so a newer host can
		// probe capabilities this plugin does not implement.
		return errorEnvelope("unknown_method", "unknown method: "+method)
	}
}

// configure replaces the live configuration from the lifecycle payload.
func (p *plugin) configure(raw []byte) error {
	request := pluginConfigRequest{}
	if len(raw) > 0 {
		if errUnmarshal := json.Unmarshal(raw, &request); errUnmarshal != nil {
			return errUnmarshal
		}
	}

	cfg := pluginConfig{}
	if len(request.ConfigYAML) > 0 {
		parsed, errParse := parsePluginConfig(request.ConfigYAML)
		if errParse != nil {
			return errParse
		}
		cfg = parsed
	}
	p.config.Store(cfg)
	return nil
}

func pluginRegistration() registration {
	return registration{
		SchemaVersion: schemaVersion,
		Metadata: metadata{
			Name:             pluginName,
			Version:          pluginVersion,
			Author:           pluginAuthor,
			GitHubRepository: pluginRepoURL,
			Logo:             "",
			ConfigFields: []configField{
				{
					Name:        "models",
					Type:        configFieldTypeArray,
					Description: "Only rewrite these model names. \"*\" wildcards allowed. Empty means all models.",
				},
				{
					Name:        "skip_models",
					Type:        configFieldTypeArray,
					Description: "Never rewrite these model names. \"*\" wildcards allowed. Wins over models.",
				},
				{
					Name:        "protocols",
					Type:        configFieldTypeArray,
					Description: "Target protocols to rewrite (to_format). Defaults to [\"openai\"].",
				},
				{
					Name:        "separator",
					Type:        configFieldTypeString,
					Description: "Join string for multiple text parts. Defaults to a newline; set to \"\" to concatenate.",
				},
				{
					Name:        "debug",
					Type:        configFieldTypeBoolean,
					Description: "Log every rewrite to stderr.",
				},
			},
		},
		Capabilities: registrationCapabilities{RequestNormalizer: true},
	}
}

// normalize implements request.normalize: it runs after CPA's native
// translation and owns the final provider payload.
func (p *plugin) normalize(raw []byte) ([]byte, error) {
	request := requestTransformRequest{}
	if len(raw) > 0 {
		if errUnmarshal := json.Unmarshal(raw, &request); errUnmarshal != nil {
			return nil, errUnmarshal
		}
	}

	cfg := p.currentConfig()
	body := request.Body

	if len(body) == 0 || !cfg.applies(request.ToFormat, request.Model) {
		return okEnvelope(payloadResponse{Body: body})
	}

	updated, stats, errFlatten := flattenAssistantContent(body, cfg.separator())
	if errFlatten != nil {
		// Fail safe: hand the untouched payload back so a malformed body can
		// never take the request down.
		logf("rewrite skipped model=%q %s->%s: %v", request.Model, request.FromFormat, request.ToFormat, errFlatten)
		return okEnvelope(payloadResponse{Body: body})
	}
	if stats.Flattened == 0 {
		if cfg.Debug {
			logf("no change model=%q %s->%s (%d message(s) scanned)", request.Model, request.FromFormat, request.ToFormat, stats.Scanned)
		}
		return okEnvelope(payloadResponse{Body: body})
	}

	if cfg.Debug {
		logf("rewrote %d/%d assistant message(s) model=%q %s->%s", stats.Flattened, stats.Scanned, request.Model, request.FromFormat, request.ToFormat)
	}
	return okEnvelope(payloadResponse{Body: updated})
}

// logf writes a plugin log line to stderr. stdout is avoided because CPA uses
// it for its own log stream.
func logf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "["+pluginID+"] "+format+"\n", args...)
}

func okEnvelope(value any) ([]byte, error) {
	result, errMarshal := json.Marshal(value)
	if errMarshal != nil {
		return nil, errMarshal
	}
	return json.Marshal(envelope{OK: true, Result: result})
}

func errorEnvelope(code, message string) ([]byte, error) {
	raw, errMarshal := json.Marshal(envelope{OK: false, Error: &envelopeError{Code: code, Message: message}})
	if errMarshal != nil {
		return nil, errMarshal
	}
	return raw, nil
}

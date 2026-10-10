# assistant-content-flatten

A [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) plugin that fixes
assistant replies being **silently dropped** by strict OpenAI-compatible
upstreams.

It is a `request.normalize` plugin: it runs after CPA's native protocol
translation and rewrites the final provider payload.

---

## The problem

CPA's "→ OpenAI Chat Completions" translators emit an assistant message's text
as an **array of content parts**:

```json
{"role": "assistant", "content": [{"type": "text", "text": "三次工具调用已完成。"}]}
```

That is valid per the OpenAI schema (assistant `content` may be a string *or* an
array of text parts), but a whole class of OpenAI-compatible upstreams — vLLM /
NIM style servers whose chat template assumes `content` is always a string —
**silently drop the message instead of rejecting the request**.

The visible symptom is confusing: the model keeps its `tool_calls` but loses its
visible text, so it reports *"I never produced that reply"* on the next turn.

Two of CPA's own translators already avoid this by flattening text-only content
before writing the payload:

| Source → OpenAI Chat Completions | Behaviour |
|---|---|
| `openai/gemini/openai_gemini_request.go` (`onlyTextContent`) | text-only → **string** ✅ |
| `openai/interactions/.../interactions_openai_request.go` (`textOnly`) | text-only → **string** ✅ |
| `openai/claude/openai_claude_request.go` | always array ❌ |
| `openai/openai/responses/openai_openai-responses_request.go` | always array ❌ |

The same is true of the request the other way: issue
[#3056](https://github.com/router-for-me/CLIProxyAPI/issues/3056) (closed as
"not planned"), and PR
[#2859](https://github.com/router-for-me/CLIProxyAPI/pull/2859) ("use string
content for assistant messages", still open).

**This plugin applies CPA's own rule to the payload, after translation**, so it
covers both missing paths without patching the proxy.

### Why configuration alone cannot do it

CPA's `payload:` rules (`default` / `override` / `filter`, plus
`default-raw` / `override-raw`) can only **set a literal value** at a JSON path
or **delete** a path. There is no way to express *"join this array's text parts
into a string"*. That is why this is a plugin and not a config snippet.

---

## What it does, exactly

For every message in the outgoing payload:

1. role must be `assistant`;
2. `content` must be a **non-empty array**;
3. **every** part must be a text part — an object with `type` absent or
   `"text"` and a string `text` field, or a bare JSON string;
4. then `content` is replaced with the joined text;
5. otherwise the message is left byte-for-byte untouched.

Deliberately **not** touched:

- any array containing a non-text part (`image_url`, `file`, `input_audio`,
  `video_url`, `refusal`, …) — multimodal payloads keep their semantics;
- `output_text` parts — that is the OpenAI **Responses** shape, where arrays are
  the required form;
- `reasoning_content` (a string already) and `tool_calls`;
- `user` / `system` / `tool` messages — OpenAI allows arrays there, and CPA
  already flattens `tool` content itself when a model declares text-only input;
- every request whose target protocol is not in `protocols` (default:
  `openai` only).

On any error the original payload is passed through unchanged, so a malformed
body can never take a request down.

---

## Install

### 1. Build

See [Build](#build) below. You need `assistant-content-flatten.so` (Linux),
`.dylib` (macOS) or `.dll` (Windows) **for the platform CPA runs on**.

### 2. Place the binary

The plugin id is derived from the file name, so the file must be named
`assistant-content-flatten.<ext>`:

```
<cpa working dir>/
└── plugins/
    └── linux/
        └── amd64/
            └── assistant-content-flatten.so
```

(`<plugins.dir>/<goos>/<goarch>/` is preferred; `<plugins.dir>/` directly also
works. Adding a `-v0.1.0` suffix to the file name is supported and is reported as
the plugin version.)

### 3. Enable it

Plugins are **disabled by default**, and they are in-process trusted code.

```yaml
plugins:
  enabled: true
  dir: "plugins"
  configs:
    assistant-content-flatten:
      enabled: true
      priority: 1
      models: ["deepseek-*"]   # recommended: scope it to the lossy upstream
      protocols: ["openai"]
```

See [`scripts/config.example.yaml`](scripts/config.example.yaml) for the
annotated version.

### 4. Restart CPA

Plugins are loaded at startup.

---

## Configuration

| Key | Type | Default | Meaning |
|---|---|---|---|
| `models` | string[] | `[]` | Only rewrite these model names. `*` wildcards allowed. Empty = all models. |
| `skip_models` | string[] | `[]` | Never rewrite these. Wins over `models`. |
| `protocols` | string[] | `["openai"]` | Target formats (`to_format`) to rewrite. |
| `separator` | string | `""` | Join string for multiple text parts. Empty concatenates, matching CPA. |
| `debug` | bool | `false` | Log every rewrite to stderr. |

`enabled` and `priority` are host-managed; unknown keys are ignored.

### Why the default separator is `""`

CPA's Gemini (`onlyTextContent`) and Interactions (`textOnly`) translators
concatenate text parts with **no** separator when they flatten text-only
content, so this plugin defaults to the same thing and produces byte-identical
output to the paths CPA already handles itself.

The one case to watch: a Claude assistant turn can carry two text blocks around
a tool call (`"pre"` / `"post"`), and concatenating those yields `"prepost"`. If
you ever see words glued together, set `separator: "\n"`.

### Scoping advice

`protocols` defaults to `openai` only — do **not** add `codex` or
`openai-response`, because those formats *require* content arrays.

Prefer scoping `models` to the upstream that actually drops array content. Every
other upstream keeps receiving the (valid) array form, so this plugin stays a
targeted workaround rather than a global behaviour change.

---

## Build

`-buildmode=c-shared` is required, so a **C compiler is needed** (gcc / clang /
MinGW-w64). `go test` and `go vet` do not need one.

### Linux / macOS

```bash
make build                     # → dist/<goos>/<goarch>/assistant-content-flatten.<ext>
make test vet
make build VERSION=0.1.0       # → assistant-content-flatten-v0.1.0.<ext>
```

### Windows

```powershell
winget install --id BrechtSanders.WinLibs.POSIX.UCRT   # MinGW-w64 gcc
.\build.ps1
```

### Cross-building a Linux `.so` (no local Linux toolchain)

CPA's runtime image is Debian-based, so build against **glibc** (bookworm) — a
musl/alpine build fails to `dlopen` at runtime.

```bash
make docker-linux-amd64
make docker-linux-arm64
```

### Building every platform in CI

[`.github/workflows/release.yml`](.github/workflows/release.yml) builds
linux/darwin/windows (amd64 + arm64) on a tag push and publishes one zip per
target plus `checksums.txt`. Use `workflow_dispatch` to produce the same
artifacts without tagging.

Each target is built on a **native runner** (`ubuntu-24.04`, `ubuntu-24.04-arm`,
`macos-15`, `macos-15-intel`, `windows-2025`). This is not stylistic:
`-buildmode=c-shared` needs a C compiler *for the target platform*, and Go
cannot cross-compile cgo without a matching cross toolchain. An earlier version
of this workflow built everything on `ubuntu-latest`; only `linux/amd64`
succeeded and the other four legs failed in the build step.

Packaging uses `pwsh` + `Compress-Archive` rather than `zip`, because the
Windows runner has neither `zip` nor a POSIX shell.

This is the practical route when the machine that runs CPA has no C compiler.

### Development

```bash
CGO_ENABLED=0 go test ./...    # 26 tests / 51 cases, no C compiler required
CGO_ENABLED=0 go vet ./...
```

`abi.go` is guarded by `//go:build cgo`, so the whole test suite runs with cgo
off. All logic lives in cgo-free files; `abi.go` is only the ABI bridge.

---

## Verifying it works

1. Enable `debug: true` and restart CPA.
2. Send a Claude Code request through CPA to the affected upstream.
3. CPA's stderr should show one line per rewritten request:

   ```
   [assistant-content-flatten] rewrote 1/6 assistant message(s) model="deepseek-…" claude->openai
   ```

4. Ask the model to repeat something it said in a previous turn. Before this
   plugin it answers *"I never said that"*; after it, it repeats the text.

---

## Notes and limitations

- The plugin only rewrites the **request** payload. It does not touch responses.
- It is a workaround for a downstream defect, not a fix for the downstream. The
  upstream should still be asked to at least *reject* rather than silently drop
  an assistant content array.
- If CPA ever grows an opt-in "text-only assistant content" compatibility mode
  (which is the shape the maintainer asked for in #3056), this plugin becomes
  redundant.
- The wire contract is mirrored locally in `wire.go` (no dependency on the
  CLIProxyAPI module). `wire_test.go` pins every JSON key, so a contract drift
  shows up as a test failure rather than silent breakage. Verified against
  CLIProxyAPI v8.0.23 (commit `d318bcc3`).

## License

MIT — see [LICENSE](LICENSE).

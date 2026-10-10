# AGENTS.md

This file provides guidance to coding agents working on this repository.

> ⚠️ **This repository is public.** Never commit internal hostnames, LAN IPs,
> deployment paths, management keys, provider API keys, or captured request logs.
> Keep deployment specifics in a gitignored `*.local.md`.

---

## 项目定位

`assistant-content-flatten` 是一个 **CLIProxyAPI（CPA）插件**，通过
`request.normalize` 钩子在协议翻译**之后**改写最终发往上游的 payload。

它解决的问题：CPA 的「→ OpenAI Chat Completions」翻译器会把 assistant 消息的
正文写成 **content-part 数组**：

```json
{"role": "assistant", "content": [{"type": "text", "text": "..."}]}
```

这**符合 OpenAI schema**（assistant content 允许是字符串或文本 part 数组），
但一类 OpenAI 兼容上游（vLLM / NIM 风格，chat template 假定 `content` 一定是字符串）
会**静默丢弃**这条消息 —— 模型保住了 `tool_calls` 却丢了正文，于是下一轮声称
「我从没说过那句话」。

CPA 自己的两条翻译路径**已经**做了「纯文本才拍平」的处理，另外两条没有：

| 源 → OpenAI Chat Completions | 行为 |
|---|---|
| `openai/gemini/openai_gemini_request.go`（`onlyTextContent`） | text-only → **字符串** ✅ |
| `openai/interactions/.../interactions_openai_request.go`（`textOnly`） | text-only → **字符串** ✅ |
| `openai/claude/openai_claude_request.go` | 总是数组 ❌ |
| `openai/openai/responses/openai_openai-responses_request.go` | 总是数组 ❌ |

本插件在翻译后补上同样的规则，**不修改 CPA 本体**。

相关上游 issue / PR：CPA [#3056](https://github.com/router-for-me/CLIProxyAPI/issues/3056)
（以 "not planned" 关闭）、[#2859](https://github.com/router-for-me/CLIProxyAPI/pull/2859)（仍 open）。

---

## 三条会「静默失败」的硬约束

这三条都**不会报错**，只会让功能悄悄不生效。改代码前先记住。

### 1. `-buildmode=c-shared` 不能交叉编译

它需要**目标平台自己的 C 编译器**。Go 在没有对应交叉工具链时无法为 cgo 交叉编译。

后果：CI matrix 里**每个目标必须跑在原生 runner 上**。
早期版本把整个 matrix 放在 `ubuntu-latest`，结果 5 个 leg 只有 `linux/amd64` 成功，
其余 4 个全部在 build 步失败。

当前 matrix（**不要"优化"回单个 ubuntu**）：

| 目标 | runner |
|---|---|
| linux/amd64 | `ubuntu-24.04` |
| linux/arm64 | `ubuntu-24.04-arm` |
| darwin/amd64 | `macos-15-intel` |
| darwin/arm64 | `macos-15` |
| windows/amd64 | `windows-2025` |

另外：Windows runner 上既没有 `zip` 也没有 POSIX shell，打包步骤用
`pwsh` + `Compress-Archive`，不要改回 `zip`。

### 2. `validPlugin` 要求四个 metadata 字段全部非空

`internal/pluginhost/host.go` 的 `validPlugin` 要求
**Name / Version / Author / GitHubRepository** 都非空，且至少声明一个 capability。

只留空 `GitHubRepository` 的后果：host 打一行
`pluginhost: plugin <id> returned invalid metadata or no capabilities` 就**丢弃插件**，
插件看起来"已加载"但完全不起作用。

`plugin_test.go` 的 `TestRegistrationSatisfiesHostValidation` 镜像了该校验，不要删。

### 3. 绝不拍平带额外字段的 text part

CPA 的 `AttachMessageCacheControl` 会把消息级 `cache_control` 挂到**最后一个
content part** 上。把那个 part 拍平成字符串会**静默丢掉缓存断点**。

因此 `assistantTextPart` 只接受**仅含 `type` / `text`** 的 part。
实测真实流量中 assistant 轮次从不出现额外字段，所以这个守卫不损失任何改写，
但兼容层**不允许丢弃它读不懂的数据**。

---

## 代码结构与职责

```
abi.go          cgo 外壳：cliproxy_plugin_init / cliproxyPluginCall / ...Free / ...Shutdown
                //go:build cgo —— CGO_ENABLED=0 时整个测试套件仍可跑
main.go         func main() {}，只为满足 -buildmode=c-shared 的 main 包要求
wire.go         ABI 契约（常量 + 本地重声明的 wire 类型），不 import CLIProxyAPI 模块
plugin.go       plugin 结构、配置原子存储、注册元数据、handleMethod、normalize、envelope、日志
config.go       pluginConfig、parsePluginConfig、applies（模型/协议门控）、wildcardMatch
normalize.go    核心：flattenAssistantContent / assistantTextPart / hasOnlyKeys
*_test.go       wire_test（JSON key 钉死）/ plugin_test（RPC round-trip）/ config_test / normalize_test
Makefile        本地 + Docker 构建；test/vet/fmt
build.ps1       Windows 构建（需 MinGW-w64 gcc）
scripts/config.example.yaml   可直接粘贴的 CPA 配置
.github/workflows/release.yml 多平台出包（tag 触发）
```

**为什么 wire 类型是本地重声明的**（不 import `github.com/router-for-me/CLIProxyAPI/v8`）：

1. ABI 就是 JSON，本地副本即线缆兼容；
2. CPA 的 go.mod 要求更新的 Go 工具链和一大坨依赖，这个 ~700 行插件不需要；
3. 插件保持零 CPA 依赖，用任意 Go ≥1.24 都能构建，无需 `replace`。

代价是**字段名必须手动对齐**。`wire_test.go` 逐字段钉死了所有 JSON key
（含 `Body` 的 base64 语义）—— 漂移会变成测试失败，而不是线上静默失效。

---

## 插件 ABI 契约速查

对照版本：**CLIProxyAPI v8.0.23（commit `d318bcc3`）**。

| 项 | 值 |
|---|---|
| ABI 版本 | `1` |
| 本插件声明的 `schema_version` | `1`（host 只拒绝"比自己新"的版本；`request.normalize` 的 payload 形状历版未变） |
| 注册方法 | `plugin.register` / `plugin.reconfigure` |
| 本插件实现的方法 | `request.normalize`（其他方法回 `unknown_method` envelope，不报错） |
| envelope | `{"ok":bool,"result":<raw>,"error":{"code","message"}}` |
| `RequestTransformRequest` | `{"FromFormat","ToFormat","Model","Stream","Body"}` —— **无 json tag**，Go 字段名即 wire key |
| `PayloadResponse` | `{"Body"}`，`[]byte` 两端都 base64 |
| `Metadata` / `ConfigField` | **无 json tag**，字段名即 wire key（`Name`/`Version`/`Author`/`GitHubRepository`/`Logo`/`ConfigFields`） |
| capability 字段 | snake_case：`request_normalizer` |

**插件 id 来自文件名**（去掉扩展名、去掉 `-v<version>` 后缀）：

```
<plugins.dir>/<goos>/<goarch>/<id>.so|.dylib|.dll
```

`<plugins.dir>` 缺省是相对 CPA 工作目录的 `plugins`。

**`ToFormat` 的取值**（`sdk/translator/formats.go`，由
`sdk/cliproxy/auth/conductor_execution.go` 的 `requestToFormat` 决定）：

```
codex / xai / meta      → "codex"
claude                  → "claude"
gemini/vertex/aistudio  → "gemini"
kimi*                   → "openai"
antigravity             → "antigravity"
devin                   → "interactions"
default                 → "openai"      ← openai-compatibility 与 cline-pass 都落这里
```

`"openai-response"`（Responses API）**几乎只作为源协议**；作为目标只在
`opts.Alt == "responses/compact"` 这一条特殊分支出现。真正往上游发 Responses
形态的是 **`"codex"`** 这个 tag。所以 `protocols` 默认 `["openai"]`，
**不要**加 `codex` / `openai-response` —— 那两个格式 content 数组是必须的。

---

## 改代码的规则

### 拍平规则的精确条件

对 payload 里每条消息：

1. `role == "assistant"`；
2. `content` 是**非空数组**；
3. **每一个** part 都是文本 part —— 对象且 `type` 缺失或为 `"text"` 且 `text` 是字符串，
   或者裸 JSON 字符串；**且不含 `type`/`text` 以外的键**；
4. 才把 `content` 换成拼接后的字符串；
5. 否则该消息**逐字节不动**。

**绝不触碰**：`reasoning_content`（本来就是字符串）、`tool_calls`、非 assistant 角色、
含 `image_url`/`file`/`input_audio`/`video_url`/`refusal` 的数组、
`output_text` part（Responses 形态，数组才是必须的）。

出错时（JSON 解析失败等）**原样回传 payload**，绝不让畸形 body 拖垮请求。

### 修改时的要求

- 逻辑改动**必须**补测试。`normalize_test.go` 里的
  `TestFlattenAssistantContentKeepsMultimodalArrays` 是防"丢图"的回归守卫
  （上游 PR #2859 就是栽在这），不要削弱它。
- 动 wire 契约就改 `wire_test.go`。
- 动注册元数据就确认 `TestRegistrationSatisfiesHostValidation` 仍过。
- 保持 `gofmt` / `go vet` 干净。
- **不要**让 `abi.go` 之外的任何文件 import `"C"` —— 否则测试就需要 C 编译器了。

### 已知的语义陷阱

- **`Model` 是客户端请求的模型名，不是上游 payload 里的名字。**
  Claude Code 请求 `deepseek-v4.1-flash` 经某 provider 出去后，上游 payload 里是
  `cline-pass/deepseek-v4.1-flash`，但插件看到的是 `deepseek-v4.1-flash`。
  带前缀的名字作为**客户端**模型名会直接 400（`unknown provider for model ...`）。
  按前者做 `models` 门控。
- `separator` 默认 `""`（直接拼接），**刻意对齐** CPA 自己的 Gemini/Interactions 行为。
  代价：一轮里 tool_use 前后的两个 text block 会粘成 `"prepost"`。
  用户可在配置里设 `separator: "\n"`。改默认值前先想清楚这一点。

---

## 测试与门禁

```bash
CGO_ENABLED=0 go test ./...     # 27 tests / 54 cases（数字随测试增加，以实跑为准）
CGO_ENABLED=0 go vet ./...
gofmt -l -e .
```

`abi.go` 带 `//go:build cgo`，所以 `CGO_ENABLED=0` 时它被排除，
整个测试套件在**没有 C 编译器**的机器上也能跑。所有逻辑都在无 cgo 的文件里。

测试分层：

| 文件 | 覆盖 |
|---|---|
| `normalize_test.go` | 拍平规则本身，含 8 个"必须不动"的反例（多模态 / cache_control / output_text …） |
| `config_test.go` | 默认值、通配匹配、模型与协议门控 |
| `plugin_test.go` | 完整 RPC round-trip：register → normalize → reconfigure，含 host 校验镜像 |
| `wire_test.go` | JSON key 与 base64 线缆语义钉死 |

---

## 构建与发布

### 本地

```bash
make build                  # → dist/<goos>/<goarch>/assistant-content-flatten.<ext>
make build VERSION=0.1.4    # → assistant-content-flatten-v0.1.4.<ext>
make docker-linux-amd64     # 交叉出 .so（glibc/bookworm 镜像）
```

Windows：`.\build.ps1`（需 MinGW-w64 gcc）。

> 本机（Windows 开发机）`go` **不在 PATH**，在 `C:\Go\bin\go.exe`；
> 且**没有 gcc**，所以本地只能 `go test` / `go vet`，出包必须走 CI 或 Docker。

### 发布

1. 改 `plugin.go` 里的 `pluginVersion`（与 tag 保持一致）；
2. 提交、推送 `main`；
3. `git tag vX.Y.Z && git push origin vX.Y.Z` → CI 出 5 平台包 + `checksums.txt`，
   并发布 GitHub Release；
4. 不想打 tag 可在 Actions 页面 `Run workflow`（`workflow_dispatch`）填版本号。

> 提交信息里含引号时，**不要**用 `git commit -m "..."`（PowerShell 会吃掉引号，
> 实测导致 commit 失败但 tag 已经推出去）。用 `git commit -F <文件>`。

---

## 部署与验证（通用流程）

> 具体主机地址、路径、备份名属于内网信息，**不要写进本仓库**。

1. 取对应平台的包，解出 `<id>.so|.dll|.dylib`；
2. 放到 `<CPA 工作目录>/plugins/<goos>/<goarch>/<id>[-vX.Y.Z].so`；
3. 在 CPA `config.yaml` 的 `plugins.configs` 下加条目（见 `scripts/config.example.yaml`）；
4. 加载方式：
   - **新增插件文件或改 config** 会触发 host 重扫 / `plugin.reconfigure`（可热加载，不必重启）；
   - 但**只替换同名已加载的 .so** 不一定触发重扫 —— 改文件名带上新版本号，或重启服务。

### 确认它真的在工作（不要只看日志）

打开 `debug: true` 后插件会往 **stderr** 打一行（systemd 场景进 journal）：

```
[assistant-content-flatten] rewrote 1/3 assistant message(s) model="…" claude->openai
```

更硬的证据是**直接看上游 payload**：CPA 的请求日志里每个 `API REQUEST` 段就是发出去的
body，检查那条 assistant 消息的 `content` 是 `str` 还是 `ARRAY`。

### 回归验证的形态

发一条 `/v1/messages` 请求，**历史里必须带一条 assistant 文本消息**，例如：

```json
{"model":"<model>","max_tokens":16,"messages":[
  {"role":"user","content":"say A"},
  {"role":"assistant","content":[{"type":"text","text":"PROBE_MARKER"}]},
  {"role":"user","content":"reply with the single letter B"}
]}
```

单轮请求没有 assistant 历史，**测不出任何东西**。再拿一个**不在 allowlist** 的
同协议模型做负对照（应保持 `ARRAY`）。

---

## 决策台账

| # | 决策 | 理由 |
|---|---|---|
| 1 | 用 `request.normalize`，不用 `RequestInterceptor` | 只有它跑在**翻译之后**，能看到 OpenAI 形态的 payload；`RequestInterceptor` 在翻译前，够不着 |
| 2 | `protocols` 默认 `["openai"]` | 只有 Chat Completions 要求字符串；`codex`/`openai-response` 需要数组 |
| 3 | `separator` 默认 `""` | 对齐 CPA 自己的 Gemini/Interactions 拼接行为；可配置 |
| 4 | 只认 `type` 为 `""`/`"text"`，**不认 `output_text`** | `output_text` 是 Responses 形态，那里数组是必须的 |
| 5 | 带额外键的 part 一律不拍平 | 不静默丢弃 `cache_control` |
| 6 | `schema_version` 声明 `1` | 只实现 `request.normalize`（历版形状未变），报最老契约可同时兼容新旧 host |
| 7 | wire 类型本地重声明，不 import CPA 模块 | 避免绑定 Go 工具链与庞大依赖图；用测试钉死 key |
| 8 | 出错时原样回传 payload | 兼容层不得让畸形输入放大成故障 |
| 9 | 不提交到 CPA 插件商店 | 自用插件；Release 仍按 `<id>_<ver>_<os>_<arch>.zip` 命名便于区分平台 |

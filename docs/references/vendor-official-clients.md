# Vendor Official Clients Reference

Official local clients, protocols and usage policies for each vendor that could
become a `subpipe` backend (ADR 0002). Researched 2026-11-10 from primary
sources; anything not verified at the source is marked **UNVERIFIED**.

Companion to `google-gemini-repos.md`.

---

## 0. Summary

| Vendor | Official local client | Local protocol | Persistent process | Subscription use from a third-party local tool |
|---|---|---|---|---|
| Google (Antigravity) | `agy`, `language_server.exe` | ConnectRPC/JSON on 127.0.0.1; ACP (`agy_acp_server.par`) | yes | **Prohibited** on consumer plans (see 1.1) |
| Anthropic | Claude Code (`claude`) | stream-json JSONL control protocol over stdio | yes (one process per session) | **Not allowed** for third-party developers; API key required (see 2.4) |
| OpenAI | Codex CLI (`codex`) | `codex app-server` JSON-RPC (JSONL over stdio) | yes | **Allowed** for open-source and personal local tools, with conditions (see 3.4) |
| DeepSeek | none for consumer plans; `dsh` harness (API key) | HTTP API; harness has ACP/SDK stdio profiles | n/a | No consumer client exists; paid API key only |
| Alibaba Qwen | Qwen Code (`qwen`) | `--acp`; stream-json over stdio | yes | Free OAuth tier discontinued 2026-04-15; API key or Coding Plan |
| Moonshot Kimi | Kimi Code CLI (`kimi`) | `kimi acp` (ACP over stdio) | yes | Kimi Code plan login or API key; policy not reviewed (**UNVERIFIED**) |
| Mistral | Mistral Vibe (`vibe`, `vibe-acp`) | ACP over stdio | yes | API key (browser sign-in creates it); plan usage is shared with the API |

**Cross-vendor transport:** most of these clients speak the Agent Client
Protocol (ACP) over stdio, natively or through an official/community adapter
(section 6). Codex app-server and Claude stream-json are vendor-specific but
more capable.

---

## 1. Google Antigravity (existing backend)

### 1.1 Policy (critical)

- FAQ, https://antigravity.google/docs/faq:
  > "Antigravity CLI headless mode (agy -p) is intended for direct scripting and
  > CI workflows. Wrapping agy inside third-party coding agents or applications,
  > or automating account rotation, is not supported under consumer plans."
  >
  > "Using third-party software, tools, or services to access Antigravity is a
  > violation of our Terms of Service… can result in suspension"
- Terms §6, https://antigravity.google/terms: same restriction; recommends a
  Gemini Enterprise or Google AI Studio API key for programmatic use.
- Enforcement reports (not primary sources):
  - router-for-me/CLIProxyAPI issue #7296: a `TOS_VIOLATION` (403) wave on
    2026-11-06, scoped to the `cloudcode-pa.googleapis.com` service.
  - discuss.ai.google.dev topic 197943: a user reports a ban after another local
    program launched the official `agy.exe` headless.

**Implication:** the current `antigravity` backend drives the official binary,
but the policy forbids wrapping that binary from a third-party application on
consumer plans. Using the official binary avoids traffic fingerprinting. It does
not make the use permitted.

---

## 2. Anthropic / Claude

### 2.1 Repositories

| Repo | Official | Language | License | Value for subpipe |
|---|---|---|---|---|
| https://github.com/anthropics/claude-agent-sdk-python | yes | Python | MIT | Reference implementation of driving the CLI: `src/claude_agent_sdk/_internal/transport/subprocess_cli.py` |
| `@anthropic-ai/claude-agent-sdk` (TypeScript) | yes | TS | see package | Same protocol, TypeScript |
| https://github.com/anthropics/anthropic-sdk-go | yes | Go | MIT (repo) | Go HTTP/SSE patterns; module is `/v2` since API version 2026-10-21 |
| https://github.com/anthropics/anthropic-cli | yes | Go | Apache-2.0 | CLI for the Claude API (API key), Go reference |
| https://github.com/Roasbeef/claude-agent-sdk-go | **no** (community) | Go | see repo | `docs/cli-protocol.md` documents the stdio protocol in Go terms |

Claude Code itself is distributed as a binary/npm package; source is not
published under an open-source license (**UNVERIFIED** for the current
release).

### 2.2 Programmatic interface

- The Agent SDK spawns:
  `claude --output-format stream-json --input-format stream-json --verbose`
  and exchanges JSONL messages over stdin/stdout, including a control protocol
  (initialize, interrupt, permission requests). One persistent subprocess
  serves one session across turns.
- Hosting guide: https://code.claude.com/docs/en/agent-sdk/hosting
- Headless mode: https://code.claude.com/docs/en/headless. For languages other
  than Python/TypeScript the docs point to `claude -p --output-format json`
  (one process per request).
- `CLAUDE_CONFIG_DIR` isolates profiles and credentials.
- `--resume` / `--session-id` continue a session.

### 2.3 Authentication

- API key (`ANTHROPIC_API_KEY`), Claude Console.
- Subscription OAuth login (Free, Pro, Max, Team/Enterprise).

### 2.4 Policy

https://code.claude.com/docs/en/legal-and-compliance ("Authentication and
credential use"):

- Subscription OAuth is "designed to support ordinary use of Claude Code and
  other native Anthropic applications".
- Developers building products, including with the Agent SDK, "should use API
  key authentication".
- Third-party developers may not "route requests through Free, Pro, Max, or
  Ultra plan credentials on behalf of their users".
- "Advertised usage limits for Pro and Max plans assume ordinary, individual
  usage of Claude Code and the Agent SDK."
- Server-side enforcement against third-party harnesses has been reported since
  January 2026 (reports, not a primary source).

**Recommended backend:** `claude` backend that spawns one persistent Claude Code
process per session and speaks stream-json over stdio. Allowed configuration:
**API key auth**. Subscription auth from subpipe falls under the restriction
above.

---

## 3. OpenAI / Codex

### 3.1 Repositories

| Repo | Official | Language | License | Value for subpipe |
|---|---|---|---|---|
| https://github.com/openai/codex | yes | Rust (`codex-rs`) | Apache-2.0 | `codex-rs/app-server/README.md`: the app-server protocol |
| `@openai/codex-sdk` (TypeScript, in openai/codex) | yes | TS | Apache-2.0 | Drives the CLI as a subprocess |
| https://github.com/openai/openai-go | yes | Go | Apache-2.0 | Go HTTP/SSE/WebSocket patterns (`/v3`, requires Go 1.26+) |

### 3.2 Programmatic interface

- **`codex app-server`** (recommended for embedding): a long-lived process
  that powers the VS Code extension and desktop app.
  - JSON-RPC 2.0 messages without the `"jsonrpc"` field, JSONL over stdio
    (default) or WebSocket/Unix socket.
  - Methods: `initialize`, `thread/start`, `thread/resume`, `thread/fork`,
    `turn/start`, `turn/steer`, `turn/interrupt`, `model/list`.
  - Notifications: `turn/started`, `item/*` (including streamed agent message
    deltas), `turn/completed`.
  - Schemas: `codex app-server generate-ts` / `generate-json-schema`, so Go
    types can be generated from the installed version.
  - Docs: https://developers.openai.com/codex/app-server and
    https://openai.com/index/unlocking-the-codex-harness
- `codex exec --json`: one-shot JSONL events; OpenAI recommends it or the Codex
  SDK for CI.
- `codex mcp-server`: experimental MCP mode.

### 3.3 Authentication

https://developers.openai.com/codex/auth: "Sign in with ChatGPT" (subscription)
or API key (usage-based). Credentials are stored under `~/.codex/`
(`auth.json`, **UNVERIFIED** for the current release).

### 3.4 Policy

- https://developers.openai.com/cookbook/articles/sign-in-with-chatgpt:
  "ChatGPT plan usage is available to open-source projects, personal projects
  that run locally, and selected private apps."
- https://developers.openai.com/siwc/token-sharing-open-source and
  https://openai.com/policies/sign-in-with-chatgpt-terms. Conditions: requests
  originate from the user's local runtime, no account rotation, and
  > "Do not provide general-purpose API access for other tools or unrelated
  > requests."

**Recommended backend:** `codex` backend that keeps one `codex app-server`
process and maps each chat completion to `thread/start` + `turn/start`,
streaming `item/*` deltas. This is the best-sanctioned surface of all vendors,
but the "general-purpose API access" clause limits subpipe's generic
OpenAI-compatible endpoint. Exposing it only to the user's own tools on
localhost is the narrowest reading. Whether that is enough is a policy
judgement, not a technical one.

---

## 4. DeepSeek

- No official consumer-subscription client. Access is through the paid API:
  - `https://api.deepseek.com` (OpenAI format) and `/anthropic` (Anthropic
    format). Docs: https://api-docs.deepseek.com/
  - Models: `deepseek-flash` (served by DeepSeek-V4.1-Flash-1017) and
    `deepseek-v4-pro`. The legacy name `deepseek-v4-flash` is still accepted.
- **DeepSeek Harness** (`dsh`), developer preview, MIT:
  https://deepseek-harness.github.io/deepseek-harness/en/guide/quickstart.
  It has `--profile acp` and `--profile sdk` JSON-RPC stdio modes and uses an
  API key.

**Recommended backend:** a plain API-key HTTP backend (`deepseek`). Driving a
client adds nothing, because API keys are the sanctioned access path and no
subscription session exists to protect. This is an exception to the
"official client only" rule and needs its own ADR if adopted.

---

## 5. Other vendors

### 5.1 Alibaba Qwen: Qwen Code

- Repo: https://github.com/QwenLM/qwen-code. Docs:
  https://qwenlm.github.io/qwen-code-docs/
- Interfaces: `qwen -p` with `--output-format json|stream-json`,
  `--input-format stream-json` (bidirectional; docs say it is "under
  construction and is intended for SDK integration"), `--include-partial-messages`,
  `--resume`/`--session-id`, and `qwen --acp` (one process hosts several
  sessions).
- Auth: "The Qwen OAuth free tier was discontinued on 2026-04-15." Current
  options are QwenCloud/Model Studio API keys, the Alibaba Cloud Coding Plan or
  Token Plan, and third-party providers.
- Approach: ACP backend (`qwen --acp`) with a Coding Plan key or API key.
  Terms of the Coding Plan were not reviewed (**UNVERIFIED**).

### 5.2 Moonshot Kimi: Kimi Code CLI

- Repo: https://github.com/MoonshotAI/kimi-code (older docs at
  MoonshotAI/kimi-cli). Docs: https://moonshotai.github.io/kimi-code/
- Interface: `kimi acp`, a multi-session ACP server over stdio. Implements the
  full core and session surface plus `session/set_model`. It returns
  `AUTH_REQUIRED` (-32000) until the user runs `kimi login`.
- Auth: Kimi Code platform login or Moonshot Open Platform API key.
- Approach: ACP backend. Policy on third-party driving was not reviewed
  (**UNVERIFIED**).

### 5.3 Mistral: Mistral Vibe

- Repo: https://github.com/mistralai/mistral-vibe (`docs/acp-setup.md`).
  Docs: https://docs.mistral.ai/vibe/code/cli/api-keys-profiles
- Interface: `vibe-acp` (ACP over stdio).
- Auth: a Mistral API key (`MISTRAL_API_KEY`, loaded from `~/.vibe/.env`);
  browser sign-in creates it. Plan usage is shared across Studio, the API and
  Vibe Code.
- Approach: because the credential is an API key, either an ACP backend
  (`vibe-acp`) or a direct API backend works.

### 5.4 Not covered

xAI Grok and Zhipu GLM were not researched in depth. Both appear in the ACP
agent list only through third-party adapters (`acp-llm-adapter` covers DeepSeek,
GLM and Groq).

---

## 6. Agent Client Protocol (ACP)

- Spec: https://agentclientprotocol.com (Zed Industries, JetBrains).
- Agent list: https://agentclientprotocol.com/get-started/agents
- Transport: JSON-RPC over stdio. The client spawns the agent as a subprocess.
- Key methods: `initialize`, `authenticate`, `session/new`, `session/load`,
  `session/prompt`, `session/cancel`; the agent streams `session/update`
  notifications. The client must answer reverse requests (`fs/*`, `terminal/*`,
  `session/request_permission`).
- Native agents: Gemini CLI (`gemini --acp`), Qwen Code (`qwen --acp`), Kimi
  Code (`kimi acp`), Mistral Vibe (`vibe-acp`), Antigravity
  (`agy_acp_server.par`), DeepSeek Harness, GitHub Copilot CLI and others.
- Adapters: Claude (`@agentclientprotocol/claude-agent-acp`, built on the
  Agent SDK) and Codex (`@agentclientprotocol/codex-acp`).

**Assessment:** a single `internal/backend/acp` client could serve several
backends (Qwen, Kimi, Mistral, DeepSeek Harness), each configured with a
command line. ACP agents are coding agents with tool calls and file access, so
the backend must deny or stub `fs/*`, `terminal/*` and permission requests to
get plain chat completions. Dedicated backends remain better for Claude
(stream-json) and Codex (app-server), whose native protocols expose more
control than the adapters.

---

## 7. Go SDK references

- `github.com/anthropics/anthropic-sdk-go/v2`: typed SSE streaming
  (`Messages.NewStreaming`, `ContentBlockDeltaEvent`/`TextDelta`), requires
  Go 1.25+.
- `github.com/openai/openai-go/v3`: Chat Completions and Responses types,
  streaming and WebSocket mode, requires Go 1.26+. Useful as the source of the
  OpenAI-compatible wire types that subpipe exposes.

These SDKs call vendor HTTP APIs. They are references for Go patterns and for
API-key backends (section 4), not for client-driving backends.

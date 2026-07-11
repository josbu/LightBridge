# Router Protocol Compatibility Contract

This document defines the compatibility boundary used by LightBridge when the
inbound client protocol differs from the selected upstream protocol. The Router
must preserve the client contract even when an OpenAI-compatible upstream emits
only a partial implementation of the Responses API.

## 1. Core boundary

LightBridge treats four concepts separately:

1. **Inbound protocol** — the HTTP/SSE contract used by the client.
2. **Client profile** — stricter parser requirements detected from stable request
   metadata, such as Claude Code, Codex CLI/App, or OpenCode.
3. **Target protocol** — the protocol implemented by the selected account.
4. **Model capability** — request fields supported by the mapped upstream model.

A client name never changes account scheduling by itself. It only selects
contract-preserving normalization after the Protocol Router has selected a
conversion path.

## 2. Client profiles

| Client profile | Stable detection signals | Required response contract |
| --- | --- | --- |
| Claude Code | `User-Agent: claude-cli/...`, `claude-code/...`, verified Claude Code request body, or `X-App` | Anthropic stream starts with `message_start`; `message_start.message.usage.input_tokens` exists; terminal `message_delta.usage` exists |
| Codex CLI | `User-Agent` containing `codex_cli_rs` or `codex-cli` | Responses terminal event contains a `response` object and canonical `response.usage` |
| Codex App | Codex user agent, `Originator`, `X-Codex-Turn-State`, or `X-Codex-Turn-Metadata` | Same terminal Responses contract; native Codex headers are preserved only on native OpenAI paths |
| OpenCode | `User-Agent`, `X-App`, or `Originator` containing `opencode` | Same terminal Responses contract |
| Generic client | no stable signal | No client-specific field invention beyond protocol invariants |

Detection is centralized in `service/router_client_profile.go`. Protocol
converters consume capability flags instead of repeating user-agent string
checks.

## 3. Anthropic Messages streaming invariants

For an Anthropic Messages client, Router output follows this semantic order:

1. `message_start`
2. zero or more content block start/delta/stop sequences
3. `message_delta`
4. `message_stop`

`message_start.message` always includes:

- `id`
- `type: "message"`
- `role: "assistant"`
- `content: []`
- `model`
- `usage.input_tokens`
- `usage.output_tokens`

If an upstream omits `response.created`, LightBridge synthesizes
`message_start` before the first recognized delta or terminal event. If no
upstream response ID or model is available at that point, Router uses a stable
request-local synthetic ID and the mapped model already stored in conversion
state.

A terminal event always emits `message_delta.usage`. Missing token counts are
represented by explicit zero values; zero is a structural fallback and is not
claimed as an estimated billing value.

## 4. OpenAI Responses terminal invariants

Strict Responses clients frequently dereference the terminal payload without
checking optional fields. For the following terminal aliases:

- `response.completed`
- `response.done`
- `response.incomplete`
- `response.failed`
- `response.cancelled`
- `response.canceled`

LightBridge guarantees:

```json
{
  "type": "response.completed",
  "response": {
    "object": "response",
    "status": "completed",
    "output": [],
    "usage": {
      "input_tokens": 0,
      "output_tokens": 0,
      "total_tokens": 0,
      "input_tokens_details": { "cached_tokens": 0 },
      "output_tokens_details": { "reasoning_tokens": 0 }
    }
  }
}
```

Existing upstream values are preserved. Prompt/completion aliases are accepted
as input and converted to Responses names. When an upstream puts `usage` or
response fields at the event root, Router copies them into the canonical
`response` wrapper.

The same normalization is applied to:

- HTTP SSE forwarding;
- HTTP non-stream Responses payloads;
- Anthropic-to-Responses bridge buffering;
- Responses WebSocket terminal events and final response objects;
- API-key passthrough paths used by strict Responses clients.

## 5. Terminal-only compatible gateway responses

Some compatible gateways buffer the model response and emit only a final
Responses event. If no incremental content block was observed and the final
`response.output` is present, the Anthropic bridge reconstructs:

- text blocks;
- thinking blocks;
- function/tool-use blocks and JSON argument deltas.

This fallback is disabled after any incremental content block has started, so a
normal stream cannot be duplicated by its final aggregate response.

## 6. Request header policy

### Native protocol paths

Native Codex/OpenAI paths retain supported client metadata, including
`originator`, `conversation_id`, `session_id`, `x-codex-turn-state`, and
`x-codex-turn-metadata`, subject to the existing allowlist and authentication
rules.

### Cross-protocol Anthropic -> third-party Responses paths

Claude Code metadata is not forwarded as if it were native Codex metadata. For
API-key accounts using the Anthropic-to-Responses bridge, Router removes:

- `originator`
- `conversation_id`
- `session_id`
- `x-codex-turn-state`
- `x-codex-turn-metadata`

It then sets a stable Router user agent and an `Accept` value matching streaming
or non-streaming behavior. Authorization, content type, trace IDs, and account
headers continue to be managed by the existing upstream request builder.

This prevents third-party gateways from selecting a Codex-specific code path
for a request whose actual body was generated from Anthropic Messages.

## 7. Grok 4.5 request policy

After model mapping, `NormalizeResponsesRequestForUpstream` applies model-family
rules. For `grok-4.5*`:

- `reasoning.effort` is retained;
- unsupported `xhigh` is reduced to `high`;
- encrypted reasoning include entries are retained;
- OpenAI-specific default `text.verbosity` is omitted;
- the OpenAI reasoning summary selector is omitted.

The policy only removes bridge-generated optional fields. It does not rewrite
user messages, tool schemas, or mapped model names.

## 8. new-api interoperability note

new-api can calculate fallback usage internally for its own billing after a
stream finishes while still forwarding the original SSE event unchanged. That
means an upstream stream may be billable in new-api but still omit
`response.usage` in the bytes received by LightBridge. Router therefore repairs
the downstream protocol shape independently of upstream billing behavior.

## 9. Testing requirements

Every new protocol bridge or terminal event alias must test:

- missing creation event;
- missing response wrapper;
- top-level usage aliases;
- completely missing usage;
- terminal-only text output;
- terminal-only tool call;
- stream and non-stream output;
- strict client detection;
- native header preservation and cross-protocol header isolation;
- WebSocket terminal normalization where the endpoint supports WebSocket.

A zero-valued structural usage fallback must never be reused as the authoritative
billing source when a separate usage accumulator or tokenizer estimate exists.

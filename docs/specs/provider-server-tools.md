# Provider Server Tools

## Status

Implemented baseline:

- Provider-side tool requests are detected in the enforcer LLM path.
- Provider-side tools are mapped to explicit Agency capabilities.
- Requests fail closed when a provider-side tool is requested without the matching grant.
- Requests fail closed when the selected model does not explicitly declare support for the requested provider-side tool capability.
- New agents receive `provider-web-search` by default.
- Higher-risk provider tools are recognized but remain denied unless explicitly granted.
- OpenAI-compatible `/v1/responses` requests are mediated by the enforcer.
- Anthropic server-side tool definitions are preserved by the Anthropic translator.
- Gemini is routed through the native `generateContent` API, with Agency's OpenAI-compatible agent request shape translated inside the enforcer.
- Gemini native streaming is translated from `streamGenerateContent` SSE to Agency-facing OpenAI-compatible chat chunks or Responses events.
- Gemini native `/v1/responses` requests are translated to native `generateContent` and returned as Responses-style output.
- Buffered provider responses produce compact audit metadata for tool type, source count, citation count, search query count, and source URLs when providers expose those fields.
- Provider-defined computer, shell, text-editor, and patch tools are classified as `agency_harnessed` but are not forwarded to providers until an executable Agency harness exists. Shell, file editing, and patch-style work should use Agency-native mediated tools instead.
- Bundled OpenAI, Anthropic, and Google provider catalog entries declare `provider_tool_capabilities` per model.
- Audit and routing metrics include provider-tool call counts, and include provider-tool estimated cost when `provider_tool_pricing` or legacy `provider_tool_costs` are configured for a model.

## Goal

Agency should be able to use provider-executed tools when they are useful, while keeping ASK boundaries intact:

- enforcement remains outside the agent boundary
- grants are explicit and externally recoverable
- provider-side execution is mediated before the provider sees the request
- tool use is auditable independently from ordinary LLM token usage
- provider-specific request and response formats normalize into a stable Agency capability model

## Provider Tool Inventory

The canonical machine-readable inventory lives at
`internal/providercatalog/provider_tools.yaml` and is exposed through
`GET /api/v1/infra/provider-tools`. The table below summarizes that inventory.
Provider names use company principals (`openai`, `anthropic`, `google`);
Gemini remains the model family and native `api_format`.

Current public provider docs show these provider-side or provider-defined tools:

| Provider | Tool families | Agency capability |
| --- | --- | --- |
| OpenAI | Web search | `provider-web-search` |
| OpenAI | File search and retrieval | `provider-file-search` |
| OpenAI | Computer use | `provider-computer-use` (not exposed; harness unavailable) |
| OpenAI | Shell, local shell | `provider-shell` (use Agency-native `execute_command`) |
| OpenAI | Apply patch | `provider-apply-patch` (use Agency-native file editing) |
| OpenAI | Tool search | `provider-tool-search` |
| OpenAI | Image generation | `provider-image-generation` |
| OpenAI | Code interpreter | `provider-code-execution` |
| OpenAI | MCP and connectors | `provider-mcp` |
| Anthropic | Web search | `provider-web-search` |
| Anthropic | Web fetch | `provider-web-fetch` |
| Anthropic | Code execution | `provider-code-execution` |
| Anthropic | Advisor | future `provider-advisor` if adopted |
| Anthropic | Tool search | `provider-tool-search` |
| Anthropic | MCP connector | `provider-mcp` |
| Anthropic | Memory | `provider-memory` |
| Anthropic | Bash | `provider-shell` (use Agency-native `execute_command`) |
| Anthropic | Computer use | `provider-computer-use` (not exposed; harness unavailable) |
| Anthropic | Text editor | `provider-text-editor` (use Agency-native file editing) |
| Gemini | Google Search grounding | `provider-web-search` |
| Gemini | URL context | `provider-url-context` |
| Gemini | Code execution | `provider-code-execution` |
| Gemini | File search | `provider-file-search` |
| Gemini | Google Maps | `provider-google-maps` |
| Gemini | Computer use | `provider-computer-use` (unconfirmed; harness unavailable) |
| xAI | Web Search | `provider-web-search` |
| xAI | X Search | future `provider-social-search` if adopted |
| xAI | Code Interpreter | `provider-code-execution` |
| xAI | Collections Search | `provider-file-search` |
| Mistral | Websearch | `provider-web-search` |
| Mistral | Code Interpreter | `provider-code-execution` |
| Mistral | Image Generation | `provider-image-generation` |
| Mistral | Document Library | `provider-file-search` |
| Perplexity | Sonar grounded web search | `provider-web-search` |
| Perplexity | Search API | `provider-web-search` or service capability, depending on integration shape |
| Perplexity | Pro Search URL fetching | `provider-web-fetch` |

## Policy

Default grants:

- `provider-web-search` is granted to new agents.

Denied unless explicitly granted:

- `provider-web-fetch`
- `provider-url-context`
- `provider-file-search`
- `provider-code-execution`
- `provider-computer-use`
- `provider-shell`
- `provider-text-editor`
- `provider-memory`
- `provider-mcp`
- `provider-image-generation`
- `provider-google-maps`
- `provider-tool-search`
- `provider-apply-patch`

Rationale:

- Search is read-oriented and already a core research expectation.
- Fetch and URL context can exfiltrate sensitive URLs or use untrusted content as context.
- File search may expose provider-hosted corpora and needs data-boundary policy.
- Code execution is a provider-hosted execution surface. Provider-defined shell, computer use, text editor, and patching require Agency execution harnesses and are not forwarded to providers by default.
- MCP/connectors can create provider-side direct access paths that bypass Agency's normal service mediation if not constrained.
- Image generation and maps may carry different data, cost, and policy obligations.

Model support:

- Provider-side tools require both an agent grant and a model declaration in `routing.yaml`.
- Model declarations use `provider_tool_capabilities`, separate from ordinary model `capabilities`.
- Missing `provider_tool_capabilities` means no provider-side tools are supported for that model.

Cost accounting:

- Model declarations should include `provider_tool_pricing` as a map from Agency provider-tool capability to structured billing metadata: unit, USD per unit, pricing source, and confidence. Legacy `provider_tool_costs` maps are still accepted as USD-per-call estimates.
- When configured, the enforcer records `provider_tool_estimated_cost_usd`, `provider_tool_cost_unit`, `provider_tool_cost_source`, and `provider_tool_cost_confidence` in the LLM audit entry.
- Routing metrics and budget views add known provider-tool estimated cost to token estimated cost.
- If pricing confidence is unknown, Agency records call counts and unknown-pricing markers so the Usage tab can surface budget risk instead of treating the tool as free.

Agency-harnessed tools:

- `provider-computer-use`, `provider-shell`, `provider-text-editor`, and `provider-apply-patch` are not treated as provider-hosted execution.
- Requests for these provider-defined tools fail closed with `provider_tool_harness_unavailable` before the provider sees the request, even if a local routing override declares model support.
- Shell and file-editing workflows already have Agency-native mediated equivalents (`execute_command`, `read_file`, `write_file`, and service/MCP tools where granted). Those paths preserve workspace boundaries, credential stripping, mediation, and audit.
- Computer-use remains unavailable until Agency has a first-class runtime/screenshot/input harness whose execution is mediated outside the agent boundary.
- Any future execution loop must translate provider proposals into Agency-native tool calls and pass through the same external mediation, consent, and audit controls as local tools. Raw shell commands, computer coordinates, edit contents, or patch payloads must not be persisted as ordinary provider-response audit metadata.

## Validation Boundary

Provider-tool behavior is covered by deterministic tests and provider response
fixtures. User-facing smoke validation should verify only that a submitted API
key works and that credential mediation is configured. It must not trigger
provider-side web search, code execution, file indexing, image generation, MCP,
computer use, shell, or other tool calls that spend quota or expand authority.

## Required Follow-up

The baseline mediates tool declaration before forwarding and records compact
evidence metadata from buffered responses. Streaming evidence extraction covers
Gemini native streams and OpenAI-compatible SSE JSON events. Additional work is
needed before provider-side tools are fully productized:

- Add provider-specific streaming evidence extraction where vendors use
  non-JSON or non-SSE formats.
- Add provider-tool support and cost metadata to non-bundled provider discovery.
- Design and implement an Agency-native computer-use execution harness before exposing provider-defined computer-use loops.

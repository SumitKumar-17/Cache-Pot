# MCP Server

::: tip v0.3.0-v0.7.0 — real
Cache-Pot runs a native MCP (Model Context Protocol) server alongside its RESP
listener, sharing the exact same in-memory state — no adapter layer.
:::

## Why this exists

Agents built on Claude, GPT, or any other MCP-compatible client need a standard way to
call `remember`/`recall`/`store_vector`/etc.-shaped tools. Instead of bolting an MCP
adapter on top of the RESP protocol, Cache-Pot's MCP server calls directly into the same
`SemanticCache`/`PromptCache`/`ToolCache`/`VectorStore`/`MemoryStore` objects the RESP
handlers use. A value written by an MCP tool call is immediately visible to a RESP
client (and vice versa) — they're two protocols over one shared memory, not two
separate systems.

## Connecting

The MCP server listens on its own port over streamable HTTP (see
[configuration](/getting-started/configuration) for `--mcp-port`/`CACHEPOT_MCP_PORT`,
default `6381`; set to `0` to disable it). Point any MCP client that supports a
streamable-HTTP server at:

```
http://<host>:6381/
```

## Tools

| Tool | Backed by | Mirrors |
|---|---|---|
| `cache_semantic_set` | `internal/semantic.SemanticCache` | `CACHE.SEMANTIC SET` |
| `cache_semantic_get` | `internal/semantic.SemanticCache` | `CACHE.SEMANTIC GET` |
| `cache_prompt_set` | `internal/semantic.PromptCache` | `CACHE.PROMPT SET` |
| `cache_prompt_get` | `internal/semantic.PromptCache` | `CACHE.PROMPT GET` |
| `tool_cache_set` | `internal/toolcache.ToolCache` | `TOOL.CACHE SET` |
| `tool_cache_get` | `internal/toolcache.ToolCache` | `TOOL.CACHE GET` |
| `store_vector` | `internal/vector.Store` | `VECTOR.UPSERT` |
| `find_similar` | `internal/vector.Store` | `VECTOR.SEARCH` (pure vector search only — no `HYBRID`) |
| `delete_vector` | `internal/vector.Store` | `VECTOR.DELETE` |
| `remember` | `internal/memory.Store` | `AGENT.REMEMBER` |
| `recall` | `internal/memory.Store` | `AGENT.RECALL` (always scoped to the calling `agent_id`, same no-cross-agent-leak guarantee) |
| `consolidate` | `internal/consolidate.Consolidator` | `SUMMARY.CREATE` |
| `extract_entities` | `internal/graph.Store` | `GRAPH.EXTRACT` (always `[0, 0]` with the mock completion provider — see [LLM completions](/getting-started/completions)) |
| `find_related` | `internal/graph.Store` | `GRAPH.RELATED` |
| `memory_history` | `internal/memory.Store` | `MEMORY.HISTORY` |

Each tool's defaults (model, temperature, similarity threshold, memory kind, etc.)
mirror its RESP command counterpart exactly — see the [command reference](/commands/)
for those defaults, and each tool's own MCP schema/description for its exact fields.

::: warning What's still NOT exposed
The original vision also described a generic `search` MCP tool (cross-memory search,
not scoped to one agent). It's available as `MEMORY.SEARCH` over RESP but isn't yet
exposed as its own MCP tool (only the always-agent-scoped `recall` is, for now).
`consolidate`/`extract_entities`/`find_related`/`memory_history` are all real now.
:::

::: warning No authentication or workspace enforcement
Every tool above calls its shared store directly, bypassing RESP's `ClientState`
entirely — there is no `AUTH` concept for MCP connections, and none of the
[workspace authorization](/getting-started/workspaces) applies here. A tool call can
read/write any workspace regardless of `--workspace-credentials`. If you're relying on
workspace isolation for real multi-tenancy, don't expose the MCP port to untrusted
callers — this is an honest, currently-unaddressed gap, not something quietly worked
around.
:::

## Example

Using any MCP client library against `http://localhost:6381/`:

```
call store_vector      { namespace: "docs", id: "a", vector: [1,0,0], text: "cats are cute" }
call find_similar       { namespace: "docs", vector: [1,0,0], k: 5 }
# -> [{ id: "a", score: 1.0 }]

call cache_semantic_set { prompt: "What is Kubernetes?", response: "K8s is a container orchestrator." }
call cache_semantic_get { prompt: "What is Kubernetes?" }
# -> "K8s is a container orchestrator."

call remember { agent_id: "research-bot", content: "User prefers concise, bullet-point summaries" }
call recall   { agent_id: "research-bot", query: "how does this user like answers formatted?" }
# -> [{ id: "...", score: 0.9x }]

call consolidate { agent_id: "research-bot" }
# -> { summary_id: "...", source_count: 5, deduped_count: 3 }

call extract_entities { workspace: "default", memory_id: "mem-1" }
call find_related      { workspace: "default", node_id: "redis" }

call memory_history { workspace: "default", id: "mem-1" }
# -> { versions: [{ version: 1, content: "..." }, { version: 2, content: "..." }] }
```

See [`internal/mcp`](https://github.com/SumitKumar-17/cache-pot/tree/main/internal/mcp)
for the implementation, built on
[`modelcontextprotocol/go-sdk`](https://github.com/modelcontextprotocol/go-sdk).

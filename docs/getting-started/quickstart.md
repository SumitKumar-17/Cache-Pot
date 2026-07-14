# Quickstart

This walks through connecting to a running Cache-Pot server and exercising
the commands that are real today. See
[Installation](/getting-started/installation) if you don't have a server
running yet.

## Connect with `redis-cli`

Cache-Pot speaks RESP2, so the standard `redis-cli` works against it — just
point it at Cache-Pot's port (`6380` by default, not Redis's `6379`, so the
two can run side by side during development):

```bash
redis-cli -p 6380 PING
# PONG

redis-cli -p 6380 SET greeting "hello"
# OK
redis-cli -p 6380 GET greeting
# "hello"

redis-cli -p 6380 EXPIRE greeting 30
# (integer) 1
redis-cli -p 6380 TTL greeting
# (integer) 30

redis-cli -p 6380 HSET user:1 name "Ada" role "engineer"
# (integer) 2
redis-cli -p 6380 HGETALL user:1
# 1) "name"
# 2) "Ada"
# 3) "role"
# 4) "engineer"

redis-cli -p 6380 LPUSH queue job-a job-b job-c
# (integer) 3
redis-cli -p 6380 LRANGE queue 0 -1
# 1) "job-c"
# 2) "job-b"
# 3) "job-a"
```

## Connect with go-redis

Because Cache-Pot implements the RESP2 protocol, standard Redis client
libraries work unmodified. For example, with
[go-redis](https://github.com/redis/go-redis):

```go
package main

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

func main() {
	ctx := context.Background()
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6380",
	})

	if err := rdb.Set(ctx, "hello", "world", 0).Err(); err != nil {
		panic(err)
	}

	val, err := rdb.Get(ctx, "hello").Result()
	if err != nil {
		panic(err)
	}
	fmt.Println(val) // "world"
}
```

Any other RESP2 client (redis-py, ioredis, jedis, node-redis, etc.) works the
same way — point it at Cache-Pot's host/port instead of Redis's.

See the [command reference](/commands/) for the full list of what's
implemented today.

## Semantic and prompt caching

These run against the default `mock` embedding provider out of the box — see
[configuration](/getting-started/configuration) to switch to `openai` for
real embeddings.

```bash
redis-cli -p 6380 CACHE.SEMANTIC SET "What is Kubernetes?" "K8s is a container orchestrator." MODEL gpt-4 COST 0.015
redis-cli -p 6380 CACHE.SEMANTIC GET "what is kubernetes" MODEL gpt-4
# "K8s is a container orchestrator."   (matched by meaning, not exact string; the hit
#                                        also records $0.015 as money saved -- see below)

redis-cli -p 6380 TOOL.CACHE SET github.getIssue '{"repo":"cache-pot","issue":42}' '{"title":"..."}' TTL 300
redis-cli -p 6380 TOOL.CACHE GET github.getIssue '{"issue":42,"repo":"cache-pot"}'
# '{"title":"..."}'   (key order in the JSON doesn't matter)
```

The example above (same words, different case) scores high similarity under both the mock
provider and real embeddings. A paraphrase using *different* words for the same concept is a
harder case for any embedding model, mock or real, and the default `THRESHOLD` may need
tuning — see the [semantic cache page](/commands/semantic-cache)'s "Tune THRESHOLD for real
embeddings" warning before assuming a miss means something is broken.

See the [semantic cache](/commands/semantic-cache) and
[tool cache](/commands/tool-cache) pages for the full command syntax.

## Vector search and MCP

```bash
redis-cli -p 6380 VECTOR.UPSERT docs a '[1,0,0]' TEXT "cats are cute"
redis-cli -p 6380 VECTOR.UPSERT docs b '[0,1,0]' TEXT "dogs are loyal"
redis-cli -p 6380 VECTOR.SEARCH docs '[1,0,0]' WITHSCORES
# 1) "a"
# 2) "1"
# 3) "b"
# 4) "0"
```

Cache-Pot also runs a native MCP server on port `6381` by default, exposing this same
vector store (plus the semantic/prompt/tool caches above) as MCP tools, sharing the
exact same in-memory state as the RESP commands above. See the
[MCP server](/getting-started/mcp-server) page.

See the [vector commands](/commands/vector) page for the full command syntax.

## Shared agent memory

Cache-Pot's actual pitch beyond "Redis clone" is shared, semantic memory for agents —
and it's real now:

```bash
redis-cli -p 6380 AGENT.REMEMBER research-bot "User prefers concise, bullet-point summaries"
redis-cli -p 6380 AGENT.RECALL research-bot "how does this user like answers formatted?"
# -> the memory above, ranked by semantic similarity

# no AGENT filter searches EVERY agent's memories in the workspace — shared, not siloed:
redis-cli -p 6380 MEMORY.SEARCH default "how does this user like answers formatted?"
```

`remember`/`recall` are also available as MCP tools — see the
[MCP server](/getting-started/mcp-server) page.

See the [agent memory commands](/commands/memory) page for the full command syntax,
including `MEMORY.PUT`/`GET` for direct control over memory ids, kinds, and metadata.

## Observability, cost analytics, and eviction

```bash
curl http://localhost:6381/metrics    # Prometheus text
curl http://localhost:6381/stats      # same data as JSON, plus cost analytics
open  http://localhost:6381/dashboard # money saved, hit rates, latency, top expensive prompts
```

The `COST 0.015` on the `CACHE.SEMANTIC SET` above is what made that hit register
$0.015 of "money saved" — see the [Observability](/getting-started/observability) page
for the full picture, including how to bound the keyspace with `--max-entries` and
`--eviction-policy`.

## Consolidation and the knowledge graph

With the default `mock` completion provider (no API key, fully offline):

```bash
redis-cli -p 6380 SUMMARY.CREATE research-bot
# "..."   -- a new long_term memory id, summarizing (and non-destructively deduping)
#            research-bot's episodic memories; the mock provider's "summary" is just a
#            deterministic echo of its input, not a real summary -- see below

redis-cli -p 6380 GRAPH.EXTRACT default mem-1
# 1) (integer) 0
# 2) (integer) 0   -- always [0, 0] with the mock provider, honestly: it can't produce
#                     the structured extraction the real thing needs
redis-cli -p 6380 GRAPH.RELATED default redis
# (empty array)
```

Both commands are real, but their *quality* depends entirely on the configured
[completion provider](/getting-started/completions). Here's the same flow with
`--completion-provider openai` — real captured output, not a hypothetical:

```bash
redis-cli -p 6380 AGENT.REMEMBER research-bot "The user asked how Redis WATCH/MULTI/EXEC optimistic locking works." KIND episodic
redis-cli -p 6380 AGENT.REMEMBER research-bot "The user followed up asking specifically what happens if a watched key changes before EXEC." KIND episodic

redis-cli -p 6380 SUMMARY.CREATE research-bot
# "a779a2677cd227ed05cc334b78ac8d4c"

redis-cli -p 6380 MEMORY.GET default a779a2677cd227ed05cc334b78ac8d4c
# ...
# content: "The user inquired about the workings of Redis's optimistic locking mechanism,
#           specifically the functions WATCH, MULTI, and EXEC, and later followed up on
#           the implications of a watched key changing before the EXEC command is
#           executed."
# metadata: {"consolidated_from_kind":"episodic","deduped_count":"2","source_count":"2"}
# ...

redis-cli -p 6380 MEMORY.PUT bot "Redis WATCH marks a key to be monitored; if it changes before EXEC, the transaction aborts." ID mem-1
redis-cli -p 6380 GRAPH.EXTRACT default mem-1
# 1) (integer) 4
# 2) (integer) 3

redis-cli -p 6380 GRAPH.RELATED default redis
# 1) "watch"
# 2) "memory:mem-1"
```

Real completion-model output isn't deterministic — exact counts and wording will vary
run to run — but real, non-trivial summarization and non-zero extraction from real
content is verified, not just claimed. See
the [Consolidation & Knowledge Graph commands](/commands/graph) page.

## Workspaces and memory versioning

```bash
redis-cli -p 6380 MEMORY.PUT research-bot "note v1" ID mem-1
redis-cli -p 6380 MEMORY.PUT research-bot "note v2" ID mem-1
redis-cli -p 6380 MEMORY.HISTORY default mem-1
# -> both versions, oldest first

./bin/cachepotd --workspace-credentials "acme:secret1,other:secret2"
redis-cli -p 6380 -a secret1 MEMORY.PUT bot "note" WORKSPACE acme
# OK -- this connection authenticated as "acme"
redis-cli -p 6380 -a secret1 MEMORY.PUT bot "note" WORKSPACE other
# (error) NOPERM this connection is not authorized for workspace "other"
```

See the [Versioning](/commands/versioning) and
[Workspaces & Multi-Tenancy](/getting-started/workspaces) pages for the full
command syntax and enforcement details.

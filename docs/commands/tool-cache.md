# Tool Cache Commands

::: tip v0.2.0 — real
This command works today.
:::

| Command | Summary |
|---|---|
| `TOOL.CACHE` | Cache/retrieve the result of an agent tool/function call by arguments |

## TOOL.CACHE

Agents frequently re-run the same tool/function calls (a GitHub API lookup, a Slack
search, a Jira query) with the same arguments across turns, and often across different
agents entirely. `TOOL.CACHE` caches tool-call results keyed by
`(tool name, canonicalized arguments)`, shared across every connection — so a second
agent asking about the same GitHub issue doesn't re-pay the API round trip.

```
TOOL.CACHE SET <tool_name> <args_json> <result> [TTL <seconds>]
TOOL.CACHE GET <tool_name> <args_json>
```

- `<args_json>` is a JSON object of the tool's call arguments, canonicalized before
  hashing so key order doesn't affect the cache key.
- `<result>` is stored as an opaque string — put whatever your tool returns (plain text,
  a JSON-encoded object, etc.) in it; Cache-Pot doesn't interpret it.
- Invalid JSON in `<args_json>` is a RESP error on both `SET` and `GET`.
- `GET` returns a nil reply on a miss (unknown or expired entry), same as `GET` on a
  missing key.

```bash
redis-cli -p 6380 TOOL.CACHE SET github.getIssue '{"repo":"cache-pot","issue":42}' '{"title":"..."}' TTL 300
redis-cli -p 6380 TOOL.CACHE GET github.getIssue '{"issue":42,"repo":"cache-pot"}'
# -> '{"title":"..."}'  (key order in the JSON doesn't matter)
```

See [`internal/toolcache`](https://github.com/SumitKumar-17/cache-pot/tree/main/internal/toolcache)
for the implementation, and [semantic cache](/commands/semantic-cache) for the related
prompt-caching commands that shipped alongside it in v0.2.0.

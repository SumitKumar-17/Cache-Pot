# Tool Cache Commands

::: info Planned — Phase 2
This command is designed but not implemented yet. See the
[roadmap](/roadmap/) for details.
:::

| Command | Summary |
|---|---|
| `TOOL.CACHE` | Cache the result of an agent tool/function call by arguments |

## What this will do

Agents frequently re-run the same tool/function calls (a GitHub API lookup,
a Slack search, a Jira query) with the same arguments across turns, and
often across different agents entirely. `TOOL.CACHE` will cache tool-call
results keyed by `(tool name, canonicalized arguments)`, shared across
agents — so a second agent asking the same question about the same GitHub
issue doesn't re-pay the API round trip (or the token cost of re-summarizing
the result).

This does not exist in the codebase today. See
[internal/toolcache](https://github.com/SumitKumar-17/cache-pot/tree/main/internal/toolcache)
for the current scaffolding, and [semantic cache](/commands/semantic-cache)
for the related prompt-caching commands landing in the same phase.

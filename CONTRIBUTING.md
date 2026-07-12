# Contributing

Cache-Pot is early — Phases 1-4 (see [ROADMAP.md](ROADMAP.md)) are done: the
Redis-compatible core, semantic/prompt/tool caching, a native vector store, shared
agent memory, and a native MCP server. Contributions are welcome, especially:

- Redis error-string compatibility fixes and Phase 1 command coverage gaps
- Test coverage (unit tests per command, integration tests against real clients)
- Documentation fixes in `docs/`
- Phase 5 groundwork (observability, cost analytics, smarter eviction — see the roadmap)

## Development

```bash
go build ./...
go vet ./...
go test ./... -race
golangci-lint run
```

## Adding a command

1. Add the handler in `internal/server/resp/handlers_<family>.go`, implemented against
   the `storage.Engine` interface (`internal/storage/engine.go`) — never depend on the
   concrete `memstore` type from a handler.
2. Register it via a `Register<Family>(r)` call added to
   `internal/server/resp/registry_all.go`, following the pattern of the existing
   `handlers_*.go` files.
3. Add its entry to `api/commands.yaml` (this is the source of truth the docs site
   renders from — don't hand-edit `docs/commands/*.md` tables, only the generated
   `_generated-table.md` include).
4. Add a unit test asserting the exact RESP wire output, including error paths.

## Commit style

Small, focused commits. Explain *why* a change is needed, not just what changed.

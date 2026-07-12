# Contributing

Cache-Pot is early — Phase 1 (see [ROADMAP.md](ROADMAP.md)) is the Redis-compatible
core. Contributions are welcome, especially:

- Phase 1 command coverage and Redis error-string compatibility fixes
- Test coverage (unit tests per command, integration tests against real clients)
- Documentation fixes in `docs/`

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
2. Register it in `internal/server/resp/dispatch.go`.
3. Add its entry to `api/commands.yaml` (this is the source of truth the docs site
   renders from — don't hand-edit `docs/commands/*.md` tables).
4. Add a unit test asserting the exact RESP wire output, including error paths.

## Commit style

Small, focused commits. Explain *why* a change is needed, not just what changed.

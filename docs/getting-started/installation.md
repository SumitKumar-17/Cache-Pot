# Installation

Cache-Pot ships as a single Go binary (`cachepotd`) with no external
dependencies — Phase 1 storage is entirely in-memory, so there is no database
or persistence layer to install alongside it.

## Option 1: Docker Compose

The fastest way to get a Cache-Pot server running:

```bash
docker compose -f deployments/compose/docker-compose.yml up --build
```

This builds the image from `deployments/docker/Dockerfile` and starts a
server listening on port `6380`, with `CACHEPOT_PORT=6380` set in the
container environment.

::: info
The compose file mounts a `cachepot-data` volume, but that volume is
currently a no-op — Phase 1 has no persistence (see
[Redis compatibility](/architecture/redis-compatibility)), so nothing is
actually written to it yet. It's reserved for a future phase.
:::

## Option 2: `go build`

If you have Go 1.24+ installed and want a local binary:

```bash
go build -o bin/cachepotd ./cmd/cachepotd
./bin/cachepotd --port 6380
```

## Option 3: `go install`

```bash
go install github.com/SumitKumar-17/cache-pot/cmd/cachepotd@latest
cachepotd --port 6380
```

(This works once the module is published/tagged; while developing against a
local checkout, prefer `go build` above.)

## Verifying it's running

Any RESP2-speaking client works. With `redis-cli`:

```bash
redis-cli -p 6380 PING
# PONG
```

Continue to [Quickstart](/getting-started/quickstart) to start using it, or
[Configuration](/getting-started/configuration) for flags and environment
variables.

# Quickstart

This walks through connecting to a running Cache-Pot server and exercising
the Phase 1 commands that are real today. See
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
implemented in Phase 1.

## A look ahead: agent memory

Cache-Pot's actual pitch beyond "Redis clone" is shared, semantic memory for
agents. That doesn't exist yet, but here's the shape of what it will look
like once Phase 4 lands:

```bash
MEMORY.PUT agent:research-bot "User prefers concise, bullet-point summaries"
MEMORY.SEARCH agent:research-bot "how does this user like answers formatted?"
```

::: info Planned — Phase 4
`MEMORY.PUT` / `MEMORY.GET` / `MEMORY.SEARCH` and `AGENT.REMEMBER` /
`AGENT.RECALL` are designed but not implemented yet. Running them against a
Phase 1 server today will fail with an unknown-command error. See the
[roadmap](/roadmap/) and [memory commands](/commands/memory) page for
details.
:::

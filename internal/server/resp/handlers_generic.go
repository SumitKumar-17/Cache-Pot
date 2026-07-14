package resp

import (
	"strconv"
	"strings"
	"time"
)

// RegisterGeneric adds the key-space-generic commands: DEL, EXISTS, EXPIRE,
// PEXPIRE, TTL, PTTL, PERSIST, TYPE, KEYS, SCAN, RENAME, FLUSHDB, FLUSHALL.
func RegisterGeneric(r *Registry) {
	r.Register(&Command{Name: "DEL", MinArgs: 2, MaxArgs: -1, Handler: handleDel})
	r.Register(&Command{Name: "EXISTS", MinArgs: 2, MaxArgs: -1, Handler: handleExists})
	r.Register(&Command{Name: "EXPIRE", MinArgs: 3, MaxArgs: 3, Handler: handleExpire})
	r.Register(&Command{Name: "PEXPIRE", MinArgs: 3, MaxArgs: 3, Handler: handlePExpire})
	r.Register(&Command{Name: "TTL", MinArgs: 2, MaxArgs: 2, Handler: handleTTL})
	r.Register(&Command{Name: "PTTL", MinArgs: 2, MaxArgs: 2, Handler: handlePTTL})
	r.Register(&Command{Name: "PERSIST", MinArgs: 2, MaxArgs: 2, Handler: handlePersist})
	r.Register(&Command{Name: "TYPE", MinArgs: 2, MaxArgs: 2, Handler: handleType})
	r.Register(&Command{Name: "KEYS", MinArgs: 2, MaxArgs: 2, Handler: handleKeys})
	r.Register(&Command{Name: "SCAN", MinArgs: 2, MaxArgs: -1, Handler: handleScan})
	r.Register(&Command{Name: "RENAME", MinArgs: 3, MaxArgs: 3, Handler: handleRename})
	r.Register(&Command{Name: "FLUSHDB", MinArgs: 1, MaxArgs: -1, Handler: handleFlushDB})
	r.Register(&Command{Name: "FLUSHALL", MinArgs: 1, MaxArgs: -1, Handler: handleFlushAll})
}

func handleDel(cs *ClientState, args []string) Reply {
	n := cs.Deps.Engine.Del(cs.Workspace, args[1:]...)
	return Int(int64(n))
}

func handleExists(cs *ClientState, args []string) Reply {
	n := cs.Deps.Engine.Exists(cs.Workspace, args[1:]...)
	return Int(int64(n))
}

func handleExpire(cs *ClientState, args []string) Reply {
	secs, err := strconv.ParseInt(args[2], 10, 64)
	if err != nil {
		return Err(ErrNotIntegerMsg)
	}
	if cs.Deps.Engine.Expire(cs.Workspace, args[1], time.Duration(secs)*time.Second) {
		return Int(1)
	}
	return Int(0)
}

func handlePExpire(cs *ClientState, args []string) Reply {
	ms, err := strconv.ParseInt(args[2], 10, 64)
	if err != nil {
		return Err(ErrNotIntegerMsg)
	}
	if cs.Deps.Engine.Expire(cs.Workspace, args[1], time.Duration(ms)*time.Millisecond) {
		return Int(1)
	}
	return Int(0)
}

func handleTTL(cs *ClientState, args []string) Reply {
	ttl, hasTTL, exists := cs.Deps.Engine.TTL(cs.Workspace, args[1])
	if !exists {
		return Int(-2)
	}
	if !hasTTL {
		return Int(-1)
	}
	secs := int64(ttl / time.Second)
	if ttl%time.Second != 0 {
		secs++ // round up a partial second, matching Redis TTL rounding
	}
	return Int(secs)
}

func handlePTTL(cs *ClientState, args []string) Reply {
	ttl, hasTTL, exists := cs.Deps.Engine.TTL(cs.Workspace, args[1])
	if !exists {
		return Int(-2)
	}
	if !hasTTL {
		return Int(-1)
	}
	return Int(int64(ttl / time.Millisecond))
}

func handlePersist(cs *ClientState, args []string) Reply {
	if cs.Deps.Engine.Persist(cs.Workspace, args[1]) {
		return Int(1)
	}
	return Int(0)
}

func handleType(cs *ClientState, args []string) Reply {
	t, ok := cs.Deps.Engine.Type(cs.Workspace, args[1])
	if !ok {
		return Simple("none")
	}
	return Simple(t)
}

func handleKeys(cs *ClientState, args []string) Reply {
	keys := cs.Deps.Engine.Keys(cs.Workspace, args[1])
	items := make([]Reply, len(keys))
	for i, k := range keys {
		items[i] = BulkString(k)
	}
	return ArraySlice(items)
}

func handleScan(cs *ClientState, args []string) Reply {
	cursor, err := strconv.ParseUint(args[1], 10, 64)
	if err != nil {
		return Err(ErrNotIntegerMsg)
	}
	match := "*"
	count := 10
	i := 2
	for i < len(args) {
		switch strings.ToUpper(args[i]) {
		case "MATCH":
			if i+1 >= len(args) {
				return Err(ErrSyntaxMsg)
			}
			match = args[i+1]
			i += 2
		case "COUNT":
			if i+1 >= len(args) {
				return Err(ErrSyntaxMsg)
			}
			c, err := strconv.Atoi(args[i+1])
			if err != nil {
				return Err(ErrNotIntegerMsg)
			}
			count = c
			i += 2
		default:
			return Err(ErrSyntaxMsg)
		}
	}
	next, keys := cs.Deps.Engine.Scan(cs.Workspace, cursor, match, count)
	items := make([]Reply, len(keys))
	for i, k := range keys {
		items[i] = BulkString(k)
	}
	return Array(BulkString(strconv.FormatUint(next, 10)), ArraySlice(items))
}

func handleRename(cs *ClientState, args []string) Reply {
	if !cs.Deps.Engine.Rename(cs.Workspace, args[1], args[2]) {
		return Err(ErrNoSuchKeyMsg)
	}
	return OK
}

func handleFlushDB(cs *ClientState, args []string) Reply {
	cs.Deps.Engine.FlushDB(cs.Workspace)
	return OK
}

// handleFlushAll flushes only the caller's own workspace, same as FLUSHDB.
// Engine has no "list all workspaces" API, so even with real multi-workspace
// auth, FLUSHALL can't iterate every workspace server-wide — it's scoped to
// the caller's own, same as every other workspace-scoped command.
func handleFlushAll(cs *ClientState, args []string) Reply {
	cs.Deps.Engine.FlushDB(cs.Workspace)
	return OK
}

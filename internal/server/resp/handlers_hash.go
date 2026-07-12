package resp

import (
	"sort"
	"strconv"
)

// RegisterHash adds the hash-type commands: HSET, HGET, HGETALL, HDEL,
// HEXISTS, HKEYS, HVALS, HLEN, HMGET, HINCRBY.
func RegisterHash(r *Registry) {
	r.Register(&Command{Name: "HSET", MinArgs: 4, MaxArgs: -1, Handler: handleHSet})
	r.Register(&Command{Name: "HGET", MinArgs: 3, MaxArgs: 3, Handler: handleHGet})
	r.Register(&Command{Name: "HGETALL", MinArgs: 2, MaxArgs: 2, Handler: handleHGetAll})
	r.Register(&Command{Name: "HDEL", MinArgs: 3, MaxArgs: -1, Handler: handleHDel})
	r.Register(&Command{Name: "HEXISTS", MinArgs: 3, MaxArgs: 3, Handler: handleHExists})
	r.Register(&Command{Name: "HKEYS", MinArgs: 2, MaxArgs: 2, Handler: handleHKeys})
	r.Register(&Command{Name: "HVALS", MinArgs: 2, MaxArgs: 2, Handler: handleHVals})
	r.Register(&Command{Name: "HLEN", MinArgs: 2, MaxArgs: 2, Handler: handleHLen})
	r.Register(&Command{Name: "HMGET", MinArgs: 3, MaxArgs: -1, Handler: handleHMGet})
	r.Register(&Command{Name: "HINCRBY", MinArgs: 4, MaxArgs: 4, Handler: handleHIncrBy})
}

func handleHSet(cs *ClientState, args []string) Reply {
	key := args[1]
	pairs := args[2:]
	if len(pairs)%2 != 0 {
		return Err(ErrWrongNumberOfArgs("hset"))
	}
	fields := make(map[string][]byte, len(pairs)/2)
	for i := 0; i < len(pairs); i += 2 {
		fields[pairs[i]] = []byte(pairs[i+1])
	}
	added, err := cs.Deps.Engine.HSet(cs.Workspace, key, fields)
	if err != nil {
		return ErrFromStorage(err)
	}
	return Int(int64(added))
}

func handleHGet(cs *ClientState, args []string) Reply {
	val, ok, err := cs.Deps.Engine.HGet(cs.Workspace, args[1], args[2])
	if err != nil {
		return ErrFromStorage(err)
	}
	if !ok {
		return NullBulk()
	}
	return Bulk(val)
}

func handleHGetAll(cs *ClientState, args []string) Reply {
	m, ok, err := cs.Deps.Engine.HGetAll(cs.Workspace, args[1])
	if err != nil {
		return ErrFromStorage(err)
	}
	if !ok {
		return ArraySlice(nil)
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	items := make([]Reply, 0, len(keys)*2)
	for _, k := range keys {
		items = append(items, BulkString(k), Bulk(m[k]))
	}
	return ArraySlice(items)
}

func handleHDel(cs *ClientState, args []string) Reply {
	n, err := cs.Deps.Engine.HDel(cs.Workspace, args[1], args[2:]...)
	if err != nil {
		return ErrFromStorage(err)
	}
	return Int(int64(n))
}

func handleHExists(cs *ClientState, args []string) Reply {
	ok, err := cs.Deps.Engine.HExists(cs.Workspace, args[1], args[2])
	if err != nil {
		return ErrFromStorage(err)
	}
	if ok {
		return Int(1)
	}
	return Int(0)
}

func handleHKeys(cs *ClientState, args []string) Reply {
	keys, _, err := cs.Deps.Engine.HKeys(cs.Workspace, args[1])
	if err != nil {
		return ErrFromStorage(err)
	}
	items := make([]Reply, len(keys))
	for i, k := range keys {
		items[i] = BulkString(k)
	}
	return ArraySlice(items)
}

func handleHVals(cs *ClientState, args []string) Reply {
	vals, _, err := cs.Deps.Engine.HVals(cs.Workspace, args[1])
	if err != nil {
		return ErrFromStorage(err)
	}
	items := make([]Reply, len(vals))
	for i, v := range vals {
		items[i] = Bulk(v)
	}
	return ArraySlice(items)
}

func handleHLen(cs *ClientState, args []string) Reply {
	n, err := cs.Deps.Engine.HLen(cs.Workspace, args[1])
	if err != nil {
		return ErrFromStorage(err)
	}
	return Int(int64(n))
}

func handleHMGet(cs *ClientState, args []string) Reply {
	vals, err := cs.Deps.Engine.HMGet(cs.Workspace, args[1], args[2:]...)
	if err != nil {
		return ErrFromStorage(err)
	}
	items := make([]Reply, len(vals))
	for i, v := range vals {
		if v == nil {
			items[i] = NullBulk()
		} else {
			items[i] = Bulk(v)
		}
	}
	return ArraySlice(items)
}

func handleHIncrBy(cs *ClientState, args []string) Reply {
	delta, err := strconv.ParseInt(args[3], 10, 64)
	if err != nil {
		return Err(ErrNotIntegerMsg)
	}
	n, err := cs.Deps.Engine.HIncrBy(cs.Workspace, args[1], args[2], delta)
	if err != nil {
		return ErrFromStorage(err)
	}
	return Int(n)
}

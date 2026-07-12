package resp

import "strconv"

// RegisterList adds the list-type commands: LPUSH, RPUSH, LPOP, RPOP,
// LRANGE, LLEN, LINDEX, LSET, LREM.
func RegisterList(r *Registry) {
	r.Register(&Command{Name: "LPUSH", MinArgs: 3, MaxArgs: -1, Handler: handleLPush})
	r.Register(&Command{Name: "RPUSH", MinArgs: 3, MaxArgs: -1, Handler: handleRPush})
	r.Register(&Command{Name: "LPOP", MinArgs: 2, MaxArgs: 2, Handler: handleLPop})
	r.Register(&Command{Name: "RPOP", MinArgs: 2, MaxArgs: 2, Handler: handleRPop})
	r.Register(&Command{Name: "LRANGE", MinArgs: 4, MaxArgs: 4, Handler: handleLRange})
	r.Register(&Command{Name: "LLEN", MinArgs: 2, MaxArgs: 2, Handler: handleLLen})
	r.Register(&Command{Name: "LINDEX", MinArgs: 3, MaxArgs: 3, Handler: handleLIndex})
	r.Register(&Command{Name: "LSET", MinArgs: 4, MaxArgs: 4, Handler: handleLSet})
	r.Register(&Command{Name: "LREM", MinArgs: 4, MaxArgs: 4, Handler: handleLRem})
}

func handleLPush(cs *ClientState, args []string) Reply {
	vals := make([][]byte, len(args)-2)
	for i, a := range args[2:] {
		vals[i] = []byte(a)
	}
	n, err := cs.Deps.Engine.LPush(cs.Workspace, args[1], vals...)
	if err != nil {
		return ErrFromStorage(err)
	}
	return Int(int64(n))
}

func handleRPush(cs *ClientState, args []string) Reply {
	vals := make([][]byte, len(args)-2)
	for i, a := range args[2:] {
		vals[i] = []byte(a)
	}
	n, err := cs.Deps.Engine.RPush(cs.Workspace, args[1], vals...)
	if err != nil {
		return ErrFromStorage(err)
	}
	return Int(int64(n))
}

func handleLPop(cs *ClientState, args []string) Reply {
	v, ok, err := cs.Deps.Engine.LPop(cs.Workspace, args[1])
	if err != nil {
		return ErrFromStorage(err)
	}
	if !ok {
		return NullBulk()
	}
	return Bulk(v)
}

func handleRPop(cs *ClientState, args []string) Reply {
	v, ok, err := cs.Deps.Engine.RPop(cs.Workspace, args[1])
	if err != nil {
		return ErrFromStorage(err)
	}
	if !ok {
		return NullBulk()
	}
	return Bulk(v)
}

func handleLRange(cs *ClientState, args []string) Reply {
	start, err := strconv.Atoi(args[2])
	if err != nil {
		return Err(ErrNotIntegerMsg)
	}
	stop, err := strconv.Atoi(args[3])
	if err != nil {
		return Err(ErrNotIntegerMsg)
	}
	vals, err := cs.Deps.Engine.LRange(cs.Workspace, args[1], start, stop)
	if err != nil {
		return ErrFromStorage(err)
	}
	items := make([]Reply, len(vals))
	for i, v := range vals {
		items[i] = Bulk(v)
	}
	return ArraySlice(items)
}

func handleLLen(cs *ClientState, args []string) Reply {
	n, err := cs.Deps.Engine.LLen(cs.Workspace, args[1])
	if err != nil {
		return ErrFromStorage(err)
	}
	return Int(int64(n))
}

func handleLIndex(cs *ClientState, args []string) Reply {
	idx, err := strconv.Atoi(args[2])
	if err != nil {
		return Err(ErrNotIntegerMsg)
	}
	v, ok, err := cs.Deps.Engine.LIndex(cs.Workspace, args[1], idx)
	if err != nil {
		return ErrFromStorage(err)
	}
	if !ok {
		return NullBulk()
	}
	return Bulk(v)
}

func handleLSet(cs *ClientState, args []string) Reply {
	idx, err := strconv.Atoi(args[2])
	if err != nil {
		return Err(ErrNotIntegerMsg)
	}
	if err := cs.Deps.Engine.LSet(cs.Workspace, args[1], idx, []byte(args[3])); err != nil {
		return ErrFromStorage(err)
	}
	return OK
}

func handleLRem(cs *ClientState, args []string) Reply {
	count, err := strconv.Atoi(args[2])
	if err != nil {
		return Err(ErrNotIntegerMsg)
	}
	n, err := cs.Deps.Engine.LRem(cs.Workspace, args[1], count, []byte(args[3]))
	if err != nil {
		return ErrFromStorage(err)
	}
	return Int(int64(n))
}

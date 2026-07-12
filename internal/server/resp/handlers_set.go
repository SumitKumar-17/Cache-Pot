package resp

// RegisterSet adds the set-type commands: SADD, SREM, SMEMBERS, SISMEMBER,
// SCARD, SINTER, SUNION, SDIFF.
func RegisterSet(r *Registry) {
	r.Register(&Command{Name: "SADD", MinArgs: 3, MaxArgs: -1, Handler: handleSAdd})
	r.Register(&Command{Name: "SREM", MinArgs: 3, MaxArgs: -1, Handler: handleSRem})
	r.Register(&Command{Name: "SMEMBERS", MinArgs: 2, MaxArgs: 2, Handler: handleSMembers})
	r.Register(&Command{Name: "SISMEMBER", MinArgs: 3, MaxArgs: 3, Handler: handleSIsMember})
	r.Register(&Command{Name: "SCARD", MinArgs: 2, MaxArgs: 2, Handler: handleSCard})
	r.Register(&Command{Name: "SINTER", MinArgs: 2, MaxArgs: -1, Handler: handleSInter})
	r.Register(&Command{Name: "SUNION", MinArgs: 2, MaxArgs: -1, Handler: handleSUnion})
	r.Register(&Command{Name: "SDIFF", MinArgs: 2, MaxArgs: -1, Handler: handleSDiff})
}

func handleSAdd(cs *ClientState, args []string) Reply {
	members := make([][]byte, len(args)-2)
	for i, a := range args[2:] {
		members[i] = []byte(a)
	}
	n, err := cs.Deps.Engine.SAdd(cs.Workspace, args[1], members...)
	if err != nil {
		return ErrFromStorage(err)
	}
	return Int(int64(n))
}

func handleSRem(cs *ClientState, args []string) Reply {
	members := make([][]byte, len(args)-2)
	for i, a := range args[2:] {
		members[i] = []byte(a)
	}
	n, err := cs.Deps.Engine.SRem(cs.Workspace, args[1], members...)
	if err != nil {
		return ErrFromStorage(err)
	}
	return Int(int64(n))
}

func handleSMembers(cs *ClientState, args []string) Reply {
	vals, err := cs.Deps.Engine.SMembers(cs.Workspace, args[1])
	if err != nil {
		return ErrFromStorage(err)
	}
	items := make([]Reply, len(vals))
	for i, v := range vals {
		items[i] = Bulk(v)
	}
	return ArraySlice(items)
}

func handleSIsMember(cs *ClientState, args []string) Reply {
	ok, err := cs.Deps.Engine.SIsMember(cs.Workspace, args[1], []byte(args[2]))
	if err != nil {
		return ErrFromStorage(err)
	}
	if ok {
		return Int(1)
	}
	return Int(0)
}

func handleSCard(cs *ClientState, args []string) Reply {
	n, err := cs.Deps.Engine.SCard(cs.Workspace, args[1])
	if err != nil {
		return ErrFromStorage(err)
	}
	return Int(int64(n))
}

func handleSInter(cs *ClientState, args []string) Reply {
	vals, err := cs.Deps.Engine.SInter(cs.Workspace, args[1:]...)
	if err != nil {
		return ErrFromStorage(err)
	}
	items := make([]Reply, len(vals))
	for i, v := range vals {
		items[i] = Bulk(v)
	}
	return ArraySlice(items)
}

func handleSUnion(cs *ClientState, args []string) Reply {
	vals, err := cs.Deps.Engine.SUnion(cs.Workspace, args[1:]...)
	if err != nil {
		return ErrFromStorage(err)
	}
	items := make([]Reply, len(vals))
	for i, v := range vals {
		items[i] = Bulk(v)
	}
	return ArraySlice(items)
}

func handleSDiff(cs *ClientState, args []string) Reply {
	vals, err := cs.Deps.Engine.SDiff(cs.Workspace, args[1:]...)
	if err != nil {
		return ErrFromStorage(err)
	}
	items := make([]Reply, len(vals))
	for i, v := range vals {
		items[i] = Bulk(v)
	}
	return ArraySlice(items)
}

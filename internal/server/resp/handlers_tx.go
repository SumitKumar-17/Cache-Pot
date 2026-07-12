package resp

// RegisterTx adds the transaction commands: MULTI, EXEC, DISCARD, WATCH,
// UNWATCH. All five are marked NoQueue so they always execute immediately,
// even while a MULTI block is open, since they control the transaction
// itself rather than being part of it.
func RegisterTx(r *Registry) {
	r.Register(&Command{Name: "MULTI", MinArgs: 1, MaxArgs: 1, NoQueue: true, Handler: handleMulti})
	r.Register(&Command{Name: "EXEC", MinArgs: 1, MaxArgs: 1, NoQueue: true, Handler: handleExec})
	r.Register(&Command{Name: "DISCARD", MinArgs: 1, MaxArgs: 1, NoQueue: true, Handler: handleDiscard})
	r.Register(&Command{Name: "WATCH", MinArgs: 2, MaxArgs: -1, NoQueue: true, Handler: handleWatch})
	r.Register(&Command{Name: "UNWATCH", MinArgs: 1, MaxArgs: 1, NoQueue: true, Handler: handleUnwatch})
}

func handleMulti(cs *ClientState, args []string) Reply {
	if cs.InMulti {
		return Err("ERR MULTI calls can not be nested")
	}
	cs.InMulti = true
	cs.MultiError = false
	cs.Queued = nil
	return OK
}

func handleDiscard(cs *ClientState, args []string) Reply {
	if !cs.InMulti {
		return Err("ERR DISCARD without MULTI")
	}
	cs.InMulti = false
	cs.MultiError = false
	cs.Queued = nil
	cs.Watched = nil
	return OK
}

func handleWatch(cs *ClientState, args []string) Reply {
	if cs.InMulti {
		return Err("ERR WATCH inside MULTI is not allowed")
	}
	if cs.Watched == nil {
		cs.Watched = make(map[string]uint64)
	}
	for _, key := range args[1:] {
		cs.Watched[key] = cs.Deps.Engine.WatchVersion(cs.Workspace, key)
	}
	return OK
}

func handleUnwatch(cs *ClientState, args []string) Reply {
	cs.Watched = nil
	return OK
}

// handleExec runs every queued command from the MULTI block. Per Redis
// semantics: if any queued command failed arity/lookup validation at queue
// time, EXEC aborts with EXECABORT without running anything; if any WATCHed
// key's version changed since WATCH, EXEC aborts by returning a null array
// (no error) without running anything; otherwise every queued command runs
// and EXEC returns an array of their replies, one per queued command.
//
// The whole run happens inside cs.Deps.Engine.Exec, which holds the store's
// global transaction mutex — see memstore.Store.globalMu's doc comment for
// why Phase 1 chose a single global lock over per-shard/key locking for
// this.
func handleExec(cs *ClientState, args []string) Reply {
	if !cs.InMulti {
		return Err("ERR EXEC without MULTI")
	}
	queued := cs.Queued
	multiErr := cs.MultiError
	watched := cs.Watched

	cs.InMulti = false
	cs.Queued = nil
	cs.MultiError = false
	cs.Watched = nil

	if multiErr {
		return Err("EXECABORT Transaction discarded because of previous errors.")
	}

	for key, ver := range watched {
		if cs.Deps.Engine.WatchVersion(cs.Workspace, key) != ver {
			return NullArray()
		}
	}

	replies := make([]Reply, 0, len(queued))
	_ = cs.Deps.Engine.Exec(func() error {
		for _, qargs := range queued {
			// cs.InMulti is already false, so Handle executes qargs
			// directly instead of re-queueing it.
			replies = append(replies, cs.Deps.Registry.Handle(cs, qargs))
		}
		return nil
	})
	return ArraySlice(replies)
}

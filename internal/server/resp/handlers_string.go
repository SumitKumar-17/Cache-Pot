package resp

import (
	"strconv"
	"strings"
	"time"

	"github.com/SumitKumar-17/cache-pot/internal/storage"
)

// RegisterString adds the string-type commands: GET, SET, MGET, MSET, INCR,
// INCRBY, DECR, DECRBY, APPEND, STRLEN.
func RegisterString(r *Registry) {
	r.Register(&Command{Name: "GET", MinArgs: 2, MaxArgs: 2, Handler: handleGet})
	r.Register(&Command{Name: "SET", MinArgs: 3, MaxArgs: -1, Handler: handleSet})
	r.Register(&Command{Name: "MGET", MinArgs: 2, MaxArgs: -1, Handler: handleMGet})
	r.Register(&Command{Name: "MSET", MinArgs: 3, MaxArgs: -1, Handler: handleMSet})
	r.Register(&Command{Name: "INCR", MinArgs: 2, MaxArgs: 2, Handler: handleIncr})
	r.Register(&Command{Name: "INCRBY", MinArgs: 3, MaxArgs: 3, Handler: handleIncrBy})
	r.Register(&Command{Name: "DECR", MinArgs: 2, MaxArgs: 2, Handler: handleDecr})
	r.Register(&Command{Name: "DECRBY", MinArgs: 3, MaxArgs: 3, Handler: handleDecrBy})
	r.Register(&Command{Name: "APPEND", MinArgs: 3, MaxArgs: 3, Handler: handleAppend})
	r.Register(&Command{Name: "STRLEN", MinArgs: 2, MaxArgs: 2, Handler: handleStrlen})
}

func handleGet(cs *ClientState, args []string) Reply {
	val, ok, err := cs.Deps.Engine.Get(cs.Workspace, args[1])
	if err != nil {
		return ErrFromStorage(err)
	}
	if !ok {
		return NullBulk()
	}
	return Bulk(val)
}

func handleSet(cs *ClientState, args []string) Reply {
	key := args[1]
	val := []byte(args[2])
	var opts storage.SetOpts

	i := 3
	for i < len(args) {
		switch strings.ToUpper(args[i]) {
		case "EX":
			if i+1 >= len(args) {
				return Err(ErrSyntaxMsg)
			}
			secs, err := strconv.ParseInt(args[i+1], 10, 64)
			if err != nil {
				return Err(ErrNotIntegerMsg)
			}
			opts.TTL = time.Duration(secs) * time.Second
			i += 2
		case "PX":
			if i+1 >= len(args) {
				return Err(ErrSyntaxMsg)
			}
			ms, err := strconv.ParseInt(args[i+1], 10, 64)
			if err != nil {
				return Err(ErrNotIntegerMsg)
			}
			opts.TTL = time.Duration(ms) * time.Millisecond
			i += 2
		case "NX":
			opts.OnlyIfNX = true
			i++
		case "XX":
			opts.OnlyIfXX = true
			i++
		case "GET":
			opts.GetOld = true
			i++
		default:
			return Err(ErrSyntaxMsg)
		}
	}
	if opts.OnlyIfNX && opts.OnlyIfXX {
		return Err(ErrSyntaxMsg)
	}

	ok, prev, hadPrev, err := cs.Deps.Engine.Set(cs.Workspace, key, val, opts)
	if err != nil {
		return ErrFromStorage(err)
	}
	if opts.GetOld {
		if hadPrev {
			return Bulk(prev)
		}
		return NullBulk()
	}
	if !ok {
		return NullBulk()
	}
	return OK
}

func handleMGet(cs *ClientState, args []string) Reply {
	keys := args[1:]
	items := make([]Reply, len(keys))
	for i, k := range keys {
		val, ok, err := cs.Deps.Engine.Get(cs.Workspace, k)
		if err != nil || !ok {
			items[i] = NullBulk()
			continue
		}
		items[i] = Bulk(val)
	}
	return ArraySlice(items)
}

func handleMSet(cs *ClientState, args []string) Reply {
	pairs := args[1:]
	if len(pairs)%2 != 0 {
		return Err(ErrWrongNumberOfArgs("mset"))
	}
	for i := 0; i < len(pairs); i += 2 {
		_, _, _, err := cs.Deps.Engine.Set(cs.Workspace, pairs[i], []byte(pairs[i+1]), storage.SetOpts{})
		if err != nil {
			return ErrFromStorage(err)
		}
	}
	return OK
}

// incrBy implements INCR/INCRBY/DECR/DECRBY as a read-parse-write sequence.
//
// Known simplification: this is not atomic under concurrent writers to the
// same key (the Get and the subsequent Set are two separate engine calls,
// not one shard-locked operation), unlike HINCRBY which holds the shard
// lock for its entire read-modify-write. Redis's real INCR is atomic;
// closing this gap would mean adding a dedicated IncrBy method to
// storage.Engine. Deferred as a tradeoff — revisit if concurrent counter
// correctness becomes a requirement.
func incrBy(cs *ClientState, key string, delta int64) (int64, error) {
	val, ok, err := cs.Deps.Engine.Get(cs.Workspace, key)
	if err != nil {
		return 0, err
	}
	cur := int64(0)
	if ok {
		n, perr := strconv.ParseInt(string(val), 10, 64)
		if perr != nil {
			return 0, storage.ErrNotInteger
		}
		cur = n
	}
	next := cur + delta
	if _, _, _, err := cs.Deps.Engine.Set(cs.Workspace, key, []byte(strconv.FormatInt(next, 10)), storage.SetOpts{}); err != nil {
		return 0, err
	}
	return next, nil
}

func handleIncr(cs *ClientState, args []string) Reply {
	n, err := incrBy(cs, args[1], 1)
	if err != nil {
		return ErrFromStorage(err)
	}
	return Int(n)
}

func handleDecr(cs *ClientState, args []string) Reply {
	n, err := incrBy(cs, args[1], -1)
	if err != nil {
		return ErrFromStorage(err)
	}
	return Int(n)
}

func handleIncrBy(cs *ClientState, args []string) Reply {
	delta, err := strconv.ParseInt(args[2], 10, 64)
	if err != nil {
		return Err(ErrNotIntegerMsg)
	}
	n, err := incrBy(cs, args[1], delta)
	if err != nil {
		return ErrFromStorage(err)
	}
	return Int(n)
}

func handleDecrBy(cs *ClientState, args []string) Reply {
	delta, err := strconv.ParseInt(args[2], 10, 64)
	if err != nil {
		return Err(ErrNotIntegerMsg)
	}
	n, err := incrBy(cs, args[1], -delta)
	if err != nil {
		return ErrFromStorage(err)
	}
	return Int(n)
}

func handleAppend(cs *ClientState, args []string) Reply {
	key := args[1]
	add := []byte(args[2])
	val, ok, err := cs.Deps.Engine.Get(cs.Workspace, key)
	if err != nil {
		return ErrFromStorage(err)
	}
	var newVal []byte
	if ok {
		newVal = append(append([]byte(nil), val...), add...)
	} else {
		newVal = add
	}
	if _, _, _, err := cs.Deps.Engine.Set(cs.Workspace, key, newVal, storage.SetOpts{}); err != nil {
		return ErrFromStorage(err)
	}
	return Int(int64(len(newVal)))
}

func handleStrlen(cs *ClientState, args []string) Reply {
	val, ok, err := cs.Deps.Engine.Get(cs.Workspace, args[1])
	if err != nil {
		return ErrFromStorage(err)
	}
	if !ok {
		return Int(0)
	}
	return Int(int64(len(val)))
}

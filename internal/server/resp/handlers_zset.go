package resp

import (
	"math"
	"strconv"
	"strings"

	"github.com/SumitKumar-17/cache-pot/internal/storage"
)

// RegisterZSet adds the sorted-set commands: ZADD, ZREM, ZRANGE, ZREVRANGE,
// ZSCORE, ZRANK, ZCARD, ZINCRBY, ZRANGEBYSCORE.
func RegisterZSet(r *Registry) {
	r.Register(&Command{Name: "ZADD", MinArgs: 4, MaxArgs: -1, Handler: handleZAdd})
	r.Register(&Command{Name: "ZREM", MinArgs: 3, MaxArgs: -1, Handler: handleZRem})
	r.Register(&Command{Name: "ZRANGE", MinArgs: 4, MaxArgs: 5, Handler: handleZRange})
	r.Register(&Command{Name: "ZREVRANGE", MinArgs: 4, MaxArgs: 5, Handler: handleZRevRange})
	r.Register(&Command{Name: "ZSCORE", MinArgs: 3, MaxArgs: 3, Handler: handleZScore})
	r.Register(&Command{Name: "ZRANK", MinArgs: 3, MaxArgs: 3, Handler: handleZRank})
	r.Register(&Command{Name: "ZCARD", MinArgs: 2, MaxArgs: 2, Handler: handleZCard})
	r.Register(&Command{Name: "ZINCRBY", MinArgs: 4, MaxArgs: 4, Handler: handleZIncrBy})
	r.Register(&Command{Name: "ZRANGEBYSCORE", MinArgs: 4, MaxArgs: 4, Handler: handleZRangeByScore})
}

func formatFloat(f float64) string {
	return strconv.FormatFloat(f, 'g', -1, 64)
}

func parseScoreArg(s string) (float64, error) {
	switch s {
	case "-inf":
		return math.Inf(-1), nil
	case "+inf", "inf":
		return math.Inf(1), nil
	}
	return strconv.ParseFloat(s, 64)
}

func zmembersReply(members []storage.ZMember, withScores bool) Reply {
	items := make([]Reply, 0, len(members)*2)
	for _, m := range members {
		items = append(items, BulkString(m.Member))
		if withScores {
			items = append(items, BulkString(formatFloat(m.Score)))
		}
	}
	return ArraySlice(items)
}

func handleZAdd(cs *ClientState, args []string) Reply {
	key := args[1]
	pairs := args[2:]
	if len(pairs) == 0 || len(pairs)%2 != 0 {
		return Err(ErrSyntaxMsg)
	}
	members := make(map[string]float64, len(pairs)/2)
	for i := 0; i < len(pairs); i += 2 {
		score, err := parseScoreArg(pairs[i])
		if err != nil {
			return Err(ErrNotFloatMsg)
		}
		members[pairs[i+1]] = score
	}
	n, err := cs.Deps.Engine.ZAdd(cs.Workspace, key, members)
	if err != nil {
		return ErrFromStorage(err)
	}
	return Int(int64(n))
}

func handleZRem(cs *ClientState, args []string) Reply {
	n, err := cs.Deps.Engine.ZRem(cs.Workspace, args[1], args[2:]...)
	if err != nil {
		return ErrFromStorage(err)
	}
	return Int(int64(n))
}

func parseZRangeArgs(args []string) (start, stop int, withScores bool, errReply Reply) {
	start, err := strconv.Atoi(args[2])
	if err != nil {
		return 0, 0, false, Err(ErrNotIntegerMsg)
	}
	stop, err = strconv.Atoi(args[3])
	if err != nil {
		return 0, 0, false, Err(ErrNotIntegerMsg)
	}
	if len(args) == 5 {
		if strings.ToUpper(args[4]) != "WITHSCORES" {
			return 0, 0, false, Err(ErrSyntaxMsg)
		}
		withScores = true
	}
	return start, stop, withScores, nil
}

func handleZRange(cs *ClientState, args []string) Reply {
	start, stop, withScores, errReply := parseZRangeArgs(args)
	if errReply != nil {
		return errReply
	}
	members, err := cs.Deps.Engine.ZRange(cs.Workspace, args[1], start, stop, withScores)
	if err != nil {
		return ErrFromStorage(err)
	}
	return zmembersReply(members, withScores)
}

func handleZRevRange(cs *ClientState, args []string) Reply {
	start, stop, withScores, errReply := parseZRangeArgs(args)
	if errReply != nil {
		return errReply
	}
	members, err := cs.Deps.Engine.ZRevRange(cs.Workspace, args[1], start, stop, withScores)
	if err != nil {
		return ErrFromStorage(err)
	}
	return zmembersReply(members, withScores)
}

func handleZScore(cs *ClientState, args []string) Reply {
	score, ok, err := cs.Deps.Engine.ZScore(cs.Workspace, args[1], args[2])
	if err != nil {
		return ErrFromStorage(err)
	}
	if !ok {
		return NullBulk()
	}
	return BulkString(formatFloat(score))
}

func handleZRank(cs *ClientState, args []string) Reply {
	rank, ok, err := cs.Deps.Engine.ZRank(cs.Workspace, args[1], args[2])
	if err != nil {
		return ErrFromStorage(err)
	}
	if !ok {
		return NullBulk()
	}
	return Int(int64(rank))
}

func handleZCard(cs *ClientState, args []string) Reply {
	n, err := cs.Deps.Engine.ZCard(cs.Workspace, args[1])
	if err != nil {
		return ErrFromStorage(err)
	}
	return Int(int64(n))
}

func handleZIncrBy(cs *ClientState, args []string) Reply {
	delta, err := parseScoreArg(args[2])
	if err != nil {
		return Err(ErrNotFloatMsg)
	}
	newScore, err := cs.Deps.Engine.ZIncrBy(cs.Workspace, args[1], args[3], delta)
	if err != nil {
		return ErrFromStorage(err)
	}
	return BulkString(formatFloat(newScore))
}

func handleZRangeByScore(cs *ClientState, args []string) Reply {
	min, err := parseScoreArg(args[2])
	if err != nil {
		return Err(ErrNotFloatMsg)
	}
	max, err := parseScoreArg(args[3])
	if err != nil {
		return Err(ErrNotFloatMsg)
	}
	members, err := cs.Deps.Engine.ZRangeByScore(cs.Workspace, args[1], min, max)
	if err != nil {
		return ErrFromStorage(err)
	}
	return zmembersReply(members, false)
}

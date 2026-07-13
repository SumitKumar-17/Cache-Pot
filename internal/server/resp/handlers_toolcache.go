package resp

import (
	"strconv"
	"strings"
	"time"

	"github.com/SumitKumar-17/cache-pot/internal/toolcache"
)

// RegisterToolCache adds TOOL.CACHE: an exact-match cache for agent
// tool-call results (e.g. a GitHub/Slack/Jira API call), keyed by
// (tool name, canonicalized arguments) and shared across all
// connections/agents.
func RegisterToolCache(r *Registry) {
	r.Register(&Command{Name: "TOOL.CACHE", MinArgs: 2, MaxArgs: -1, Handler: handleToolCache})
}

func handleToolCache(cs *ClientState, args []string) Reply {
	switch strings.ToUpper(args[1]) {
	case "SET":
		return handleToolCacheSet(cs, args)
	case "GET":
		return handleToolCacheGet(cs, args)
	default:
		return Err(ErrSyntaxMsg)
	}
}

// handleToolCacheSet implements:
//
//	TOOL.CACHE SET <tool_name> <args_json> <result> [TTL <seconds>]
func handleToolCacheSet(cs *ClientState, args []string) Reply {
	// args: TOOL.CACHE SET <tool_name> <args_json> <result> [TTL secs]
	if len(args) < 5 {
		return Err(ErrWrongNumberOfArgs("tool.cache"))
	}
	toolName := args[2]
	argsJSON := args[3]
	result := args[4]

	var ttl time.Duration
	for i := 5; i < len(args); i += 2 {
		if i+1 >= len(args) {
			return Err(ErrSyntaxMsg)
		}
		switch strings.ToUpper(args[i]) {
		case "TTL":
			secs, err := strconv.ParseInt(args[i+1], 10, 64)
			if err != nil {
				return Err(ErrNotIntegerMsg)
			}
			if secs > 0 {
				ttl = time.Duration(secs) * time.Second
			} else {
				ttl = 0
			}
		default:
			return Err(ErrSyntaxMsg)
		}
	}

	key, err := toolcache.ToolKey(toolName, argsJSON)
	if err != nil {
		return Err(ErrInvalidToolArgsJSONMsg)
	}
	cs.Deps.ToolCache.Set(key, result, ttl)
	return OK
}

// handleToolCacheGet implements:
//
//	TOOL.CACHE GET <tool_name> <args_json>
func handleToolCacheGet(cs *ClientState, args []string) Reply {
	if len(args) != 4 {
		return Err(ErrWrongNumberOfArgs("tool.cache"))
	}
	toolName := args[2]
	argsJSON := args[3]

	key, err := toolcache.ToolKey(toolName, argsJSON)
	if err != nil {
		return Err(ErrInvalidToolArgsJSONMsg)
	}

	result, found := cs.Deps.ToolCache.Get(key)
	if !found {
		cs.Deps.Metrics.ToolCacheMiss()
		return NullBulk()
	}
	cs.Deps.Metrics.ToolCacheHit()
	return BulkString(result)
}

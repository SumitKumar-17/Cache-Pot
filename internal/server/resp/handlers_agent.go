package resp

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/SumitKumar-17/cache-pot/internal/memory"
)

// RegisterAgent adds the agent-ergonomics convenience commands AGENT.REMEMBER
// and AGENT.RECALL, both thin wrappers around the same MemoryStore that
// backs MEMORY.PUT/MEMORY.GET/MEMORY.SEARCH (see handlers_memory.go). They
// exist purely to make the common "just remember this" / "recall my own
// memories" agent-side calls more ergonomic than the fuller MEMORY.* forms --
// no new store logic lives here.
func RegisterAgent(r *Registry) {
	r.Register(&Command{Name: "AGENT.REMEMBER", MinArgs: 3, MaxArgs: -1, Handler: handleAgentRemember})
	r.Register(&Command{Name: "AGENT.RECALL", MinArgs: 3, MaxArgs: -1, Handler: handleAgentRecall})
}

// handleAgentRemember implements:
//
//	AGENT.REMEMBER <agent_id> <content> [WORKSPACE <workspace>]
//	               [KIND short_term|long_term|episodic|semantic]
//	               [METADATA <metadata_json>] [TTL <seconds>]
//
// Identical behavior to MEMORY.PUT (same workspace/kind defaults), except
// there is no ID option: AGENT.REMEMBER always generates a new memory id, so
// it's the simple "just remember this" entry point rather than an
// upsert-by-id tool -- MEMORY.PUT ... ID <id> remains the way to do that.
// Returns the generated id as a bulk string, same as MEMORY.PUT.
func handleAgentRemember(cs *ClientState, args []string) Reply {
	agentID := args[1]
	content := args[2]

	workspace := defaultMemoryWorkspace
	kind := defaultMemoryKind
	var metadata map[string]string
	var ttl time.Duration

	for i := 3; i < len(args); i += 2 {
		if i+1 >= len(args) {
			return Err(ErrSyntaxMsg)
		}
		switch strings.ToUpper(args[i]) {
		case "WORKSPACE":
			workspace = args[i+1]
		case "KIND":
			k, ok := memory.ParseKind(args[i+1])
			if !ok {
				return Err(ErrSyntaxMsg)
			}
			kind = k
		case "METADATA":
			m, err := parseMetadataJSON(args[i+1])
			if err != nil {
				return Err(ErrInvalidMetadataJSONMsg)
			}
			metadata = m
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

	if !cs.authorizedForWorkspace(workspace) {
		return Err(ErrWorkspaceNotAuthorized(workspace))
	}

	m := memory.Memory{
		AgentID:     agentID,
		WorkspaceID: workspace,
		Kind:        kind,
		Content:     content,
		Metadata:    metadata,
	}

	storedID, err := cs.Deps.MemoryStore.Put(context.Background(), m, ttl)
	if err != nil {
		return Err("ERR " + err.Error())
	}
	cs.Deps.Metrics.MemoryWrite()
	return BulkString(storedID)
}

// handleAgentRecall implements:
//
//	AGENT.RECALL <agent_id> <query> [WORKSPACE <workspace>] [KIND <kind>]
//	             [K <n>] [THRESHOLD <float>] [WITHSCORES]
//
// Identical to MEMORY.SEARCH <workspace> <query> ... WITHSCORES, except
// agent_id is always applied as the AGENT filter: this is "recall this
// agent's own memories", the ergonomic counterpart to MEMORY.SEARCH's
// workspace-wide, optionally-agent-filtered search. Same reply shape as
// MEMORY.SEARCH.
func handleAgentRecall(cs *ClientState, args []string) Reply {
	agentID := args[1]
	query := args[2]

	workspace := defaultMemoryWorkspace
	var kind *memory.Kind
	k := defaultMemorySearchK
	var threshold *float64
	withScores := false

	for i := 3; i < len(args); {
		switch strings.ToUpper(args[i]) {
		case "WORKSPACE":
			if i+1 >= len(args) {
				return Err(ErrSyntaxMsg)
			}
			workspace = args[i+1]
			i += 2
		case "KIND":
			if i+1 >= len(args) {
				return Err(ErrSyntaxMsg)
			}
			kd, ok := memory.ParseKind(args[i+1])
			if !ok {
				return Err(ErrSyntaxMsg)
			}
			kind = &kd
			i += 2
		case "K":
			if i+1 >= len(args) {
				return Err(ErrSyntaxMsg)
			}
			n, err := strconv.Atoi(args[i+1])
			if err != nil {
				return Err(ErrNotIntegerMsg)
			}
			k = n
			i += 2
		case "THRESHOLD":
			if i+1 >= len(args) {
				return Err(ErrSyntaxMsg)
			}
			t, err := strconv.ParseFloat(args[i+1], 64)
			if err != nil {
				return Err(ErrNotFloatMsg)
			}
			threshold = &t
			i += 2
		case "WITHSCORES":
			withScores = true
			i++
		default:
			return Err(ErrSyntaxMsg)
		}
	}

	if !cs.authorizedForWorkspace(workspace) {
		return Err(ErrWorkspaceNotAuthorized(workspace))
	}

	results, err := cs.Deps.MemoryStore.Search(context.Background(), workspace, query, memory.SearchOptions{
		AgentID:   agentID,
		Kind:      kind,
		K:         k,
		Threshold: threshold,
	})
	if err != nil {
		return Err("ERR " + err.Error())
	}
	cs.Deps.Metrics.MemoryRead()

	items := make([]Reply, 0, len(results)*2)
	for _, r := range results {
		items = append(items, BulkString(r.ID))
		if withScores {
			items = append(items, BulkString(formatFloat(r.Score)))
		}
	}
	return ArraySlice(items)
}

package resp

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/SumitKumar-17/cache-pot/internal/memory"
)

// Defaults for MEMORY.PUT/MEMORY.SEARCH's optional arguments, applied when
// the corresponding keyword is omitted.
const (
	defaultMemoryWorkspace = "default"
	defaultMemoryKind      = memory.LongTerm
	defaultMemorySearchK   = 10
)

// RegisterMemory adds the agent-memory commands: MEMORY.PUT, MEMORY.GET,
// MEMORY.SEARCH, and MEMORY.HISTORY, backed by internal/memory's shared
// agent-memory store. Embedding-similarity ranking reuses
// internal/vector.Store internally (see internal/memory/store.go).
func RegisterMemory(r *Registry) {
	r.Register(&Command{Name: "MEMORY.PUT", MinArgs: 3, MaxArgs: -1, Handler: handleMemoryPut})
	r.Register(&Command{Name: "MEMORY.GET", MinArgs: 3, MaxArgs: 3, Handler: handleMemoryGet})
	r.Register(&Command{Name: "MEMORY.SEARCH", MinArgs: 3, MaxArgs: -1, Handler: handleMemorySearch})
	r.Register(&Command{Name: "MEMORY.HISTORY", MinArgs: 3, MaxArgs: 5, Handler: handleMemoryHistory})
}

// handleMemoryPut implements:
//
//	MEMORY.PUT <agent_id> <content> [ID <id>] [WORKSPACE <workspace>]
//	           [KIND short_term|long_term|episodic|semantic]
//	           [METADATA <metadata_json>] [TTL <seconds>]
//
// Embeds content via the shared embed.Provider and stores the resulting
// vector for MEMORY.SEARCH to rank against. Returns the memory's id as a
// bulk string -- whether it was generated or the caller's own ID was used --
// so callers always get back a definite id to reference later. Putting to
// an ID that already exists in workspace bumps that memory's Version and
// replaces its content/embedding/metadata in place, keeping the prior
// version retrievable via MEMORY.HISTORY (see internal/memory/store.go's
// doc comment).
func handleMemoryPut(cs *ClientState, args []string) Reply {
	agentID := args[1]
	content := args[2]

	id := ""
	workspace := defaultMemoryWorkspace
	kind := defaultMemoryKind
	var metadata map[string]string
	var ttl time.Duration

	for i := 3; i < len(args); i += 2 {
		if i+1 >= len(args) {
			return Err(ErrSyntaxMsg)
		}
		switch strings.ToUpper(args[i]) {
		case "ID":
			id = args[i+1]
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
		ID:          id,
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

// handleMemoryGet implements:
//
//	MEMORY.GET <workspace> <id>
//
// Returns a flat RESP array of field/value pairs (the same convention as
// HGETALL): id, agent_id, kind, content, metadata (re-serialized as a JSON
// string), created_at (RFC3339), version. Returns a nil array (RESP2's
// "*-1", via NullArray) if id doesn't exist in workspace or has expired --
// a GET-by-id miss is a missing-key signal, distinct from "found but
// empty", unlike MEMORY.SEARCH's empty-array convention for "found nothing
// to rank" below.
func handleMemoryGet(cs *ClientState, args []string) Reply {
	workspace := args[1]
	id := args[2]

	if !cs.authorizedForWorkspace(workspace) {
		return Err(ErrWorkspaceNotAuthorized(workspace))
	}

	m, found, err := cs.Deps.MemoryStore.Get(context.Background(), workspace, id)
	if err != nil {
		return Err("ERR " + err.Error())
	}
	cs.Deps.Metrics.MemoryRead()
	if !found {
		return NullArray()
	}

	fields, err := memoryFieldsReply(m)
	if err != nil {
		return Err("ERR " + err.Error())
	}
	return ArraySlice(fields)
}

// memoryFieldsReply builds the flat field/value RESP array items (id,
// agent_id, kind, content, metadata, created_at, version) describing a
// single memory version -- the same convention as HGETALL. Shared by
// handleMemoryGet and handleMemoryHistory so a single memory version is
// formatted identically by both commands.
func memoryFieldsReply(m memory.Memory) ([]Reply, error) {
	metadata := m.Metadata
	if metadata == nil {
		metadata = map[string]string{}
	}
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return nil, err
	}
	return []Reply{
		BulkString("id"), BulkString(m.ID),
		BulkString("agent_id"), BulkString(m.AgentID),
		BulkString("kind"), BulkString(m.Kind.String()),
		BulkString("content"), BulkString(m.Content),
		BulkString("metadata"), BulkString(string(metadataJSON)),
		BulkString("created_at"), BulkString(m.CreatedAt.Format(time.RFC3339)),
		BulkString("version"), BulkString(strconv.Itoa(m.Version)),
	}, nil
}

// handleMemorySearch implements:
//
//	MEMORY.SEARCH <workspace> <query> [AGENT <agent_id>] [KIND <kind>]
//	              [K <n>] [THRESHOLD <float>] [WITHSCORES]
//
// Without AGENT, ranks across every agent's memories in workspace -- the
// "shared memory" pillar's payoff: no artificial per-agent silo by default.
// With AGENT, scoped to just that agent's own memories (AGENT.RECALL is the
// more ergonomic, always-scoped way to do that; this filter exists here for
// completeness). KIND (optional) filters
// to a single memory kind. K (default 10) caps the number of results.
// THRESHOLD (optional, unset by default) is a minimum cosine-similarity
// score a result must meet -- unlike CACHE.SEMANTIC's hard threshold gate,
// MEMORY.SEARCH defaults to "give me the best K matches" regardless of
// score, since there's no single natural similarity cutoff across
// arbitrary memory content the way there is for a fixed cache-hit decision.
// Returns ids ranked best-first (or id, score pairs with WITHSCORES), the
// same reply shape as VECTOR.SEARCH. Empty array (not nil) if nothing
// matches.
func handleMemorySearch(cs *ClientState, args []string) Reply {
	workspace := args[1]
	query := args[2]

	if !cs.authorizedForWorkspace(workspace) {
		return Err(ErrWorkspaceNotAuthorized(workspace))
	}

	var agentID string
	var kind *memory.Kind
	k := defaultMemorySearchK
	var threshold *float64
	withScores := false

	for i := 3; i < len(args); {
		switch strings.ToUpper(args[i]) {
		case "AGENT":
			if i+1 >= len(args) {
				return Err(ErrSyntaxMsg)
			}
			agentID = args[i+1]
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

// handleMemoryHistory implements:
//
//	MEMORY.HISTORY <workspace> <id> [LIMIT <n>]
//
// Returns every stored version of (workspace, id) oldest-first, ending with
// the current/latest version, as an array of per-version snapshots -- each
// snapshot formatted exactly like MEMORY.GET's own flat field-array reply
// (see memoryFieldsReply). LIMIT (optional) caps how many of the MOST
// RECENT versions are returned when there are more than LIMIT: if limited,
// the oldest surviving entries are still ordered oldest-first among
// themselves, but the oldest versions overall are the ones dropped -- a
// caller asking to limit history wants the most recent lineage, not the
// most ancient. Returns a nil array (via NullArray), not an empty one, if
// id doesn't exist in workspace or has expired -- the same "missing key"
// convention MEMORY.GET uses for a specific-id lookup that finds nothing,
// distinct from MEMORY.SEARCH's empty-array convention for a search that
// finds nothing (see handleMemoryGet's doc comment).
func handleMemoryHistory(cs *ClientState, args []string) Reply {
	workspace := args[1]
	id := args[2]

	if !cs.authorizedForWorkspace(workspace) {
		return Err(ErrWorkspaceNotAuthorized(workspace))
	}

	limit := 0 // <= 0 means "no limit beyond the store's own internal cap"
	if len(args) > 3 {
		if len(args) != 5 || !strings.EqualFold(args[3], "LIMIT") {
			return Err(ErrSyntaxMsg)
		}
		n, err := strconv.Atoi(args[4])
		if err != nil {
			return Err(ErrNotIntegerMsg)
		}
		limit = n
	}

	versions, err := cs.Deps.MemoryStore.History(context.Background(), workspace, id)
	if err != nil {
		return Err("ERR " + err.Error())
	}
	cs.Deps.Metrics.MemoryRead()
	if versions == nil {
		return NullArray()
	}

	if limit > 0 && len(versions) > limit {
		versions = versions[len(versions)-limit:]
	}

	items := make([]Reply, 0, len(versions))
	for _, v := range versions {
		fields, err := memoryFieldsReply(v)
		if err != nil {
			return Err("ERR " + err.Error())
		}
		items = append(items, ArraySlice(fields))
	}
	return ArraySlice(items)
}

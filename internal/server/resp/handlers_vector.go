package resp

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/SumitKumar-17/cache-pot/internal/vector"
)

// Defaults for VECTOR.SEARCH's optional arguments, applied when the
// corresponding keyword is omitted.
const (
	defaultSearchK     = 10
	defaultHybridAlpha = 0.5
)

// RegisterVector adds the native vector-store commands: VECTOR.UPSERT,
// VECTOR.SEARCH, and VECTOR.DELETE, backed by internal/vector's flat
// (brute-force) index, partitioned by namespace.
func RegisterVector(r *Registry) {
	r.Register(&Command{Name: "VECTOR.UPSERT", MinArgs: 4, MaxArgs: -1, Handler: handleVectorUpsert})
	r.Register(&Command{Name: "VECTOR.SEARCH", MinArgs: 3, MaxArgs: -1, Handler: handleVectorSearch})
	r.Register(&Command{Name: "VECTOR.DELETE", MinArgs: 3, MaxArgs: 3, Handler: handleVectorDelete})
}

// parseVectorJSON decodes s as a JSON array of numbers into a []float32.
// Invalid JSON, or valid JSON that isn't an array of numbers (e.g. an
// object, or an array containing a string), is reported as an error.
func parseVectorJSON(s string) ([]float32, error) {
	var raw []float64
	if err := json.Unmarshal([]byte(s), &raw); err != nil {
		return nil, err
	}
	vec := make([]float32, len(raw))
	for i, v := range raw {
		vec[i] = float32(v)
	}
	return vec, nil
}

// metadataValueToString canonicalizes one JSON metadata value into the
// string form VECTOR.SEARCH's FILTER compares against. Strings are used
// verbatim (so `FILTER color red` matches a metadata value of the JSON
// string "red"); every other JSON type (number, bool, null, or a nested
// object/array) falls back to its JSON text form (e.g. the number 3
// becomes "3"). This is a deliberate simplification -- see VECTOR.SEARCH's
// FILTER documentation -- rather than a principled type-aware comparison.
func metadataValueToString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

// parseMetadataJSON decodes s as a JSON object into a map[string]string,
// canonicalizing each value via metadataValueToString. Invalid JSON, or
// valid JSON that isn't an object, is reported as an error.
func parseMetadataJSON(s string) (map[string]string, error) {
	var raw map[string]any
	if err := json.Unmarshal([]byte(s), &raw); err != nil {
		return nil, err
	}
	out := make(map[string]string, len(raw))
	for k, v := range raw {
		out[k] = metadataValueToString(v)
	}
	return out, nil
}

func parseMetric(s string) (vector.DistanceMetric, bool) {
	switch strings.ToUpper(s) {
	case "COSINE":
		return vector.Cosine, true
	case "DOT":
		return vector.Dot, true
	case "EUCLIDEAN":
		return vector.Euclidean, true
	default:
		return 0, false
	}
}

// handleVectorUpsert implements:
//
//	VECTOR.UPSERT <namespace> <id> <vector_json> [METADATA <metadata_json>] [TEXT <text>]
func handleVectorUpsert(cs *ClientState, args []string) Reply {
	// args: VECTOR.UPSERT <namespace> <id> <vector_json> [opts...]
	namespace := args[1]
	id := args[2]
	vec, err := parseVectorJSON(args[3])
	if err != nil {
		return Err(ErrInvalidVectorJSONMsg)
	}

	var metadata map[string]string
	var text string

	for i := 4; i < len(args); {
		switch strings.ToUpper(args[i]) {
		case "METADATA":
			if i+1 >= len(args) {
				return Err(ErrSyntaxMsg)
			}
			metadata, err = parseMetadataJSON(args[i+1])
			if err != nil {
				return Err(ErrInvalidMetadataJSONMsg)
			}
			i += 2
		case "TEXT":
			if i+1 >= len(args) {
				return Err(ErrSyntaxMsg)
			}
			text = args[i+1]
			i += 2
		default:
			return Err(ErrSyntaxMsg)
		}
	}

	cs.Deps.VectorStore.Upsert(namespace, id, vec, metadata, text)
	return OK
}

// handleVectorSearch implements:
//
//	VECTOR.SEARCH <namespace> <vector_json> [K <n>] [METRIC cosine|dot|euclidean]
//	              [FILTER <key> <value> ...] [HYBRID <query_text> [ALPHA <float>]]
//	              [WITHSCORES]
//
// Results are a RESP array of ids (or id, score, id, score, ... pairs if
// WITHSCORES is given), best-match-first, capped at K. An unknown namespace
// or no matches returns an empty array, never an error.
func handleVectorSearch(cs *ClientState, args []string) Reply {
	// args: VECTOR.SEARCH <namespace> <vector_json> [opts...]
	namespace := args[1]
	vec, err := parseVectorJSON(args[2])
	if err != nil {
		return Err(ErrInvalidVectorJSONMsg)
	}

	k := defaultSearchK
	metric := vector.Cosine
	var filter map[string]string
	var hybrid *vector.HybridOpts
	withScores := false

	for i := 3; i < len(args); {
		switch strings.ToUpper(args[i]) {
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
		case "METRIC":
			if i+1 >= len(args) {
				return Err(ErrSyntaxMsg)
			}
			m, ok := parseMetric(args[i+1])
			if !ok {
				return Err(ErrSyntaxMsg)
			}
			metric = m
			i += 2
		case "FILTER":
			if i+2 >= len(args) {
				return Err(ErrSyntaxMsg)
			}
			if filter == nil {
				filter = make(map[string]string)
			}
			filter[args[i+1]] = args[i+2]
			i += 3
		case "HYBRID":
			if i+1 >= len(args) {
				return Err(ErrSyntaxMsg)
			}
			hybrid = &vector.HybridOpts{QueryText: args[i+1], Alpha: defaultHybridAlpha}
			i += 2
			if i < len(args) && strings.ToUpper(args[i]) == "ALPHA" {
				if i+1 >= len(args) {
					return Err(ErrSyntaxMsg)
				}
				a, err := strconv.ParseFloat(args[i+1], 64)
				if err != nil {
					return Err(ErrNotFloatMsg)
				}
				hybrid.Alpha = a
				i += 2
			}
		case "WITHSCORES":
			withScores = true
			i++
		default:
			return Err(ErrSyntaxMsg)
		}
	}

	results := cs.Deps.VectorStore.Search(namespace, vec, k, metric, filter, hybrid)
	cs.Deps.Metrics.VectorSearchPerformed()

	items := make([]Reply, 0, len(results)*2)
	for _, res := range results {
		items = append(items, BulkString(res.ID))
		if withScores {
			items = append(items, BulkString(formatFloat(res.Score)))
		}
	}
	return ArraySlice(items)
}

// handleVectorDelete implements:
//
//	VECTOR.DELETE <namespace> <id>
//
// Returns :1 if id existed in namespace and was removed, :0 otherwise
// (mirrors Redis DEL's integer-count convention, scaled to a single key).
func handleVectorDelete(cs *ClientState, args []string) Reply {
	namespace := args[1]
	id := args[2]
	if cs.Deps.VectorStore.Delete(namespace, id) {
		return Int(1)
	}
	return Int(0)
}

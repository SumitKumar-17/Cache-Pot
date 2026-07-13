package resp

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/SumitKumar-17/cache-pot/internal/semantic"
)

// Defaults for CACHE.SEMANTIC's optional arguments, applied when the
// corresponding keyword is omitted.
const (
	defaultSemanticModel     = "default"
	defaultSemanticTemp      = "0"
	defaultSemanticThreshold = 0.85
)

// RegisterSemantic adds the LLM-response cache commands: CACHE.SEMANTIC
// (similarity-based, model/temperature/prompt-aware) and CACHE.PROMPT
// (exact-match, keyed by template + variables + model).
func RegisterSemantic(r *Registry) {
	r.Register(&Command{Name: "CACHE.SEMANTIC", MinArgs: 2, MaxArgs: -1, Handler: handleCacheSemantic})
	r.Register(&Command{Name: "CACHE.PROMPT", MinArgs: 2, MaxArgs: -1, Handler: handleCachePrompt})
}

func handleCacheSemantic(cs *ClientState, args []string) Reply {
	switch strings.ToUpper(args[1]) {
	case "SET":
		return handleCacheSemanticSet(cs, args)
	case "GET":
		return handleCacheSemanticGet(cs, args)
	default:
		return Err(ErrSyntaxMsg)
	}
}

// handleCacheSemanticSet implements:
//
//	CACHE.SEMANTIC SET <prompt> <response> [MODEL <model>] [TEMP <temperature>] [TTL <seconds>] [COST <dollars>]
func handleCacheSemanticSet(cs *ClientState, args []string) Reply {
	// args: CACHE.SEMANTIC SET <prompt> <response> [opts...]
	if len(args) < 4 {
		return Err(ErrWrongNumberOfArgs("cache.semantic"))
	}
	prompt := args[2]
	response := args[3]
	model := defaultSemanticModel
	temp := defaultSemanticTemp
	var ttl time.Duration
	var cost float64

	for i := 4; i < len(args); i += 2 {
		if i+1 >= len(args) {
			return Err(ErrSyntaxMsg)
		}
		switch strings.ToUpper(args[i]) {
		case "MODEL":
			model = args[i+1]
		case "TEMP":
			if _, err := strconv.ParseFloat(args[i+1], 64); err != nil {
				return Err(ErrNotFloatMsg)
			}
			temp = args[i+1]
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
		case "COST":
			c, err := parseCostArg(args[i+1])
			if err != nil {
				return Err(err.Error())
			}
			cost = c
		default:
			return Err(ErrSyntaxMsg)
		}
	}

	if err := cs.Deps.SemanticCache.Set(context.Background(), prompt, model, temp, response, ttl, cost); err != nil {
		return Err("ERR " + err.Error())
	}
	return OK
}

// handleCacheSemanticGet implements:
//
//	CACHE.SEMANTIC GET <prompt> [MODEL <model>] [TEMP <temperature>] [THRESHOLD <float>]
func handleCacheSemanticGet(cs *ClientState, args []string) Reply {
	// args: CACHE.SEMANTIC GET <prompt> [opts...]
	if len(args) < 3 {
		return Err(ErrWrongNumberOfArgs("cache.semantic"))
	}
	prompt := args[2]
	model := defaultSemanticModel
	temp := defaultSemanticTemp
	threshold := defaultSemanticThreshold

	for i := 3; i < len(args); i += 2 {
		if i+1 >= len(args) {
			return Err(ErrSyntaxMsg)
		}
		switch strings.ToUpper(args[i]) {
		case "MODEL":
			model = args[i+1]
		case "TEMP":
			if _, err := strconv.ParseFloat(args[i+1], 64); err != nil {
				return Err(ErrNotFloatMsg)
			}
			temp = args[i+1]
		case "THRESHOLD":
			t, err := strconv.ParseFloat(args[i+1], 64)
			if err != nil {
				return Err(ErrNotFloatMsg)
			}
			threshold = t
		default:
			return Err(ErrSyntaxMsg)
		}
	}

	response, found, cost, err := cs.Deps.SemanticCache.Get(context.Background(), prompt, model, temp, threshold)
	if err != nil {
		return Err("ERR " + err.Error())
	}
	if !found {
		cs.Deps.Metrics.SemanticCacheMiss()
		return NullBulk()
	}
	cs.Deps.Metrics.SemanticCacheHit()
	if cost > 0 {
		cs.Deps.Analytics.RecordCacheHitSavings("semantic", prompt, cost)
	}
	return BulkString(response)
}

// parseCostArg parses a CACHE.SEMANTIC/CACHE.PROMPT SET COST argument: a
// non-negative float, in dollars. A negative or non-numeric value is
// rejected outright rather than silently clamped, so a caller's mistake
// doesn't get folded into money-saved accounting unnoticed.
func parseCostArg(s string) (float64, error) {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, errors.New(ErrNotFloatMsg)
	}
	if v < 0 {
		return 0, errors.New(ErrCostNegativeMsg)
	}
	return v, nil
}

func handleCachePrompt(cs *ClientState, args []string) Reply {
	switch strings.ToUpper(args[1]) {
	case "SET":
		return handleCachePromptSet(cs, args)
	case "GET":
		return handleCachePromptGet(cs, args)
	default:
		return Err(ErrSyntaxMsg)
	}
}

// handleCachePromptSet implements:
//
//	CACHE.PROMPT SET <template> <variables_json> <model> <response> [TTL <seconds>] [COST <dollars>]
func handleCachePromptSet(cs *ClientState, args []string) Reply {
	// args: CACHE.PROMPT SET <template> <variables_json> <model> <response> [TTL secs] [COST dollars]
	if len(args) < 6 {
		return Err(ErrWrongNumberOfArgs("cache.prompt"))
	}
	template := args[2]
	variablesJSON := args[3]
	model := args[4]
	response := args[5]

	var ttl time.Duration
	var cost float64
	for i := 6; i < len(args); i += 2 {
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
		case "COST":
			c, err := parseCostArg(args[i+1])
			if err != nil {
				return Err(err.Error())
			}
			cost = c
		default:
			return Err(ErrSyntaxMsg)
		}
	}

	key, err := semantic.TemplateKey(template, variablesJSON, model)
	if err != nil {
		return Err(ErrInvalidJSONMsg)
	}
	cs.Deps.PromptCache.Set(key, response, ttl, cost)
	return OK
}

// handleCachePromptGet implements:
//
//	CACHE.PROMPT GET <template> <variables_json> <model>
func handleCachePromptGet(cs *ClientState, args []string) Reply {
	if len(args) != 5 {
		return Err(ErrWrongNumberOfArgs("cache.prompt"))
	}
	template := args[2]
	variablesJSON := args[3]
	model := args[4]

	key, err := semantic.TemplateKey(template, variablesJSON, model)
	if err != nil {
		return Err(ErrInvalidJSONMsg)
	}

	response, found, cost := cs.Deps.PromptCache.Get(key)
	if !found {
		cs.Deps.Metrics.PromptCacheMiss()
		return NullBulk()
	}
	cs.Deps.Metrics.PromptCacheHit()
	if cost > 0 {
		cs.Deps.Analytics.RecordCacheHitSavings("prompt", template, cost)
	}
	return BulkString(response)
}

// Package mcp implements Phase 3's native MCP (Model Context Protocol)
// server: it exposes Cache-Pot's already-implemented cache/vector-store/
// agent-memory capabilities as MCP tools, operating directly against the
// same shared SemanticCache/PromptCache/ToolCache/VectorStore/MemoryStore
// instances the RESP server uses (see internal/server/resp.Deps). There is
// no adapter layer and no separate process or storage -- an MCP tool call
// and a RESP command are two front doors onto the exact same in-memory
// state.
//
// Only tools backed by genuinely-implemented functionality are registered
// here. The original product vision also imagined a summarize()-style tool,
// but that maps to Phase 6 (consolidation), which is not implemented yet
// (internal/consolidate is still an empty skeleton) -- so it is deliberately
// absent rather than faked. Phase 4 (agent memory, internal/memory) is now
// real, so its remember()/recall() tools are exposed below alongside the
// cache/vector tools. See each RegisterXxx function below for the tools
// this phase does expose.
package mcp

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/SumitKumar-17/cache-pot/internal/analytics"
	"github.com/SumitKumar-17/cache-pot/internal/memory"
	"github.com/SumitKumar-17/cache-pot/internal/observability"
	"github.com/SumitKumar-17/cache-pot/internal/semantic"
	"github.com/SumitKumar-17/cache-pot/internal/toolcache"
	"github.com/SumitKumar-17/cache-pot/internal/vector"
)

// Defaults mirrored from internal/server/resp/handlers_semantic.go and
// handlers_vector.go so MCP clients and RESP clients see identical
// defaulting behavior for the same optional parameters.
const (
	defaultSemanticModel     = "default"
	defaultSemanticTemp      = "0"
	defaultSemanticThreshold = 0.85
	defaultSearchK           = 10

	// defaultMemoryWorkspace, defaultMemoryKind, and defaultMemorySearchK
	// mirror internal/server/resp/handlers_memory.go and
	// handlers_agent.go's own defaults, so remember/recall behave
	// identically to AGENT.REMEMBER/AGENT.RECALL when their optional
	// parameters are omitted.
	defaultMemoryWorkspace = "default"
	defaultMemoryKind      = memory.LongTerm
	defaultMemorySearchK   = 10

	serverName    = "cachepot"
	serverVersion = "0.3.0"
)

// Server exposes Cache-Pot's Phase 1-4 caches, vector store, and agent
// memory as MCP tools. It holds no state of its own beyond the shared
// objects passed to New: every tool reads/writes directly through
// semanticCache/promptCache/toolCache/vectorStore/memoryStore, the exact
// same instances threaded into resp.Deps by internal/server/server.go, so
// an MCP client and a RESP client observe the same cache/vector-store/
// memory contents.
type Server struct {
	semanticCache *semantic.SemanticCache
	promptCache   *semantic.PromptCache
	toolCache     *toolcache.ToolCache
	vectorStore   *vector.Store
	memoryStore   *memory.Store
	metrics       *observability.Metrics
	analytics     *analytics.Tracker

	sdk *sdkmcp.Server
}

// New builds an MCP Server backed by the given shared cache/store instances.
// Callers (internal/server/server.go) must pass the exact same objects used
// to build resp.Deps -- constructing new semantic.SemanticCache/
// semantic.PromptCache/toolcache.ToolCache/vector.Store/memory.Store
// instances here instead would silently create a second, disconnected
// memory space, defeating the entire point of "no adapter layer". metrics
// should likewise be the same *observability.Metrics the RESP listener
// records into, so /metrics and /stats reflect both protocols' traffic;
// tracker should likewise be the same *analytics.Tracker, so money-saved
// and embedding-cost figures reflect both protocols' traffic too.
func New(semanticCache *semantic.SemanticCache, promptCache *semantic.PromptCache, toolCache *toolcache.ToolCache, vectorStore *vector.Store, memoryStore *memory.Store, metrics *observability.Metrics, tracker *analytics.Tracker) *Server {
	s := &Server{
		semanticCache: semanticCache,
		promptCache:   promptCache,
		toolCache:     toolCache,
		vectorStore:   vectorStore,
		memoryStore:   memoryStore,
		metrics:       metrics,
		analytics:     tracker,
	}
	s.sdk = sdkmcp.NewServer(&sdkmcp.Implementation{
		Name:    serverName,
		Version: serverVersion,
	}, &sdkmcp.ServerOptions{
		Instructions: "Cache-Pot exposes its semantic/prompt/tool response caches, native vector store, and shared agent memory (remember/recall) as MCP tools, backed by the exact same in-memory state as its RESP server.",
	})
	s.registerCacheTools()
	s.registerVectorTools()
	s.registerMemoryTools()
	return s
}

// Handler returns an http.Handler serving the MCP streamable-HTTP transport
// (the 2025-03-26 MCP spec's streamable HTTP transport). It is meant to be
// mounted on a ServeMux inside the same long-lived cachepotd process that
// runs the RESP listener, not spawned as a per-client subprocess -- a stdio
// transport would give each MCP client its own disconnected process and
// therefore its own disconnected memory, which is exactly what this package
// exists to avoid.
func (s *Server) Handler() http.Handler {
	return sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server {
		return s.sdk
	}, nil)
}

// parseMetric parses a case-insensitive distance-metric name the same way
// internal/server/resp/handlers_vector.go's parseMetric does, defaulting to
// vector.Cosine when s is empty.
func parseMetric(s string) (vector.DistanceMetric, error) {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "", "COSINE":
		return vector.Cosine, nil
	case "DOT":
		return vector.Dot, nil
	case "EUCLIDEAN":
		return vector.Euclidean, nil
	default:
		return 0, fmt.Errorf("mcp: unknown metric %q (want \"cosine\", \"dot\", or \"euclidean\")", s)
	}
}

// ttlFromSeconds converts a TTL expressed in seconds (as used by every
// RESP TTL option) into a time.Duration, mirroring the RESP handlers'
// convention that a non-positive value means "never expires".
func ttlFromSeconds(secs int) time.Duration {
	if secs > 0 {
		return time.Duration(secs) * time.Second
	}
	return 0
}

// ---- CACHE.SEMANTIC ----

// CacheSemanticSetInput is the input for cache_semantic_set, mirroring
// CACHE.SEMANTIC SET <prompt> <response> [MODEL <model>] [TEMP <temperature>]
// [TTL <seconds>].
type CacheSemanticSetInput struct {
	Prompt   string `json:"prompt" jsonschema:"the prompt text to cache a response for"`
	Response string `json:"response" jsonschema:"the LLM response to associate with this prompt"`
	// Model and Temp default to "default" and "0" respectively when empty,
	// matching CACHE.SEMANTIC SET's defaults when MODEL/TEMP are omitted.
	Model      string `json:"model,omitempty" jsonschema:"model-name partition; defaults to \"default\" if omitted or empty"`
	Temp       string `json:"temp,omitempty" jsonschema:"temperature partition, as a decimal string; defaults to \"0\" if omitted or empty"`
	TTLSeconds int    `json:"ttl_seconds,omitempty" jsonschema:"entry lifetime in seconds; omitted or <=0 means the entry never expires"`
	// Cost is the optional, caller-reported dollar cost of originally
	// producing Response (e.g. the LLM completion cost the caller paid).
	// Omitted or <= 0 means "unknown/not reported" -- a later hit will
	// never record fabricated money-saved.
	Cost float64 `json:"cost,omitempty" jsonschema:"optional dollar cost of originally producing this response, used to track money saved on future cache hits; omitted or <=0 means unknown/not reported"`
}

// CacheSemanticSetOutput is the output for cache_semantic_set.
type CacheSemanticSetOutput struct {
	OK bool `json:"ok"`
}

func (s *Server) cacheSemanticSet(ctx context.Context, _ *sdkmcp.CallToolRequest, in CacheSemanticSetInput) (*sdkmcp.CallToolResult, CacheSemanticSetOutput, error) {
	s.metrics.MCPCallRecorded("cache_semantic_set")
	model := in.Model
	if model == "" {
		model = defaultSemanticModel
	}
	temp := in.Temp
	if temp == "" {
		temp = defaultSemanticTemp
	} else if _, err := strconv.ParseFloat(temp, 64); err != nil {
		return nil, CacheSemanticSetOutput{}, fmt.Errorf("mcp: temp %q is not a valid float", temp)
	}
	if in.Cost < 0 {
		return nil, CacheSemanticSetOutput{}, fmt.Errorf("mcp: cost %v must be non-negative", in.Cost)
	}

	if err := s.semanticCache.Set(ctx, in.Prompt, model, temp, in.Response, ttlFromSeconds(in.TTLSeconds), in.Cost); err != nil {
		return nil, CacheSemanticSetOutput{}, err
	}
	return nil, CacheSemanticSetOutput{OK: true}, nil
}

// CacheSemanticGetInput is the input for cache_semantic_get, mirroring
// CACHE.SEMANTIC GET <prompt> [MODEL <model>] [TEMP <temperature>]
// [THRESHOLD <float>].
type CacheSemanticGetInput struct {
	Prompt string `json:"prompt" jsonschema:"the prompt text to look up"`
	Model  string `json:"model,omitempty" jsonschema:"model-name partition; defaults to \"default\" if omitted or empty"`
	Temp   string `json:"temp,omitempty" jsonschema:"temperature partition, as a decimal string; defaults to \"0\" if omitted or empty"`
	// Threshold is a pointer so an explicit 0 (accept any similarity score,
	// however low) can be distinguished from "omitted" (use the 0.85
	// default), unlike a plain float64 field where both would be the JSON
	// zero value.
	Threshold *float64 `json:"threshold,omitempty" jsonschema:"minimum cosine similarity in [-1,1] required to count as a hit; defaults to 0.85 if omitted"`
}

// CacheSemanticGetOutput is the output for cache_semantic_get.
type CacheSemanticGetOutput struct {
	Found    bool   `json:"found"`
	Response string `json:"response,omitempty"`
}

func (s *Server) cacheSemanticGet(ctx context.Context, _ *sdkmcp.CallToolRequest, in CacheSemanticGetInput) (*sdkmcp.CallToolResult, CacheSemanticGetOutput, error) {
	s.metrics.MCPCallRecorded("cache_semantic_get")
	model := in.Model
	if model == "" {
		model = defaultSemanticModel
	}
	temp := in.Temp
	if temp == "" {
		temp = defaultSemanticTemp
	} else if _, err := strconv.ParseFloat(temp, 64); err != nil {
		return nil, CacheSemanticGetOutput{}, fmt.Errorf("mcp: temp %q is not a valid float", temp)
	}
	threshold := defaultSemanticThreshold
	if in.Threshold != nil {
		threshold = *in.Threshold
	}

	response, found, cost, err := s.semanticCache.Get(ctx, in.Prompt, model, temp, threshold)
	if err != nil {
		return nil, CacheSemanticGetOutput{}, err
	}
	if found {
		s.metrics.SemanticCacheHit()
		if cost > 0 {
			s.analytics.RecordCacheHitSavings("semantic", in.Prompt, cost)
		}
	} else {
		s.metrics.SemanticCacheMiss()
	}
	return nil, CacheSemanticGetOutput{Found: found, Response: response}, nil
}

// ---- CACHE.PROMPT ----

// CachePromptSetInput is the input for cache_prompt_set, mirroring
// CACHE.PROMPT SET <template> <variables_json> <model> <response>
// [TTL <seconds>].
type CachePromptSetInput struct {
	Template      string `json:"template" jsonschema:"the prompt template text"`
	VariablesJSON string `json:"variables_json" jsonschema:"a JSON object of the template variables used to fill in Template"`
	Model         string `json:"model" jsonschema:"model name this cached response is scoped to"`
	Response      string `json:"response" jsonschema:"the LLM response to cache"`
	TTLSeconds    int    `json:"ttl_seconds,omitempty" jsonschema:"entry lifetime in seconds; omitted or <=0 means the entry never expires"`
	// Cost mirrors CacheSemanticSetInput.Cost -- see its doc comment.
	Cost float64 `json:"cost,omitempty" jsonschema:"optional dollar cost of originally producing this response, used to track money saved on future cache hits; omitted or <=0 means unknown/not reported"`
}

// CachePromptSetOutput is the output for cache_prompt_set.
type CachePromptSetOutput struct {
	OK bool `json:"ok"`
}

func (s *Server) cachePromptSet(_ context.Context, _ *sdkmcp.CallToolRequest, in CachePromptSetInput) (*sdkmcp.CallToolResult, CachePromptSetOutput, error) {
	s.metrics.MCPCallRecorded("cache_prompt_set")
	if in.Cost < 0 {
		return nil, CachePromptSetOutput{}, fmt.Errorf("mcp: cost %v must be non-negative", in.Cost)
	}
	key, err := semantic.TemplateKey(in.Template, in.VariablesJSON, in.Model)
	if err != nil {
		return nil, CachePromptSetOutput{}, fmt.Errorf("mcp: invalid variables_json: %w", err)
	}
	s.promptCache.Set(key, in.Response, ttlFromSeconds(in.TTLSeconds), in.Cost)
	return nil, CachePromptSetOutput{OK: true}, nil
}

// CachePromptGetInput is the input for cache_prompt_get, mirroring
// CACHE.PROMPT GET <template> <variables_json> <model>.
type CachePromptGetInput struct {
	Template      string `json:"template" jsonschema:"the prompt template text"`
	VariablesJSON string `json:"variables_json" jsonschema:"a JSON object of the template variables used to fill in Template"`
	Model         string `json:"model" jsonschema:"model name this cached response is scoped to"`
}

// CachePromptGetOutput is the output for cache_prompt_get.
type CachePromptGetOutput struct {
	Found    bool   `json:"found"`
	Response string `json:"response,omitempty"`
}

func (s *Server) cachePromptGet(_ context.Context, _ *sdkmcp.CallToolRequest, in CachePromptGetInput) (*sdkmcp.CallToolResult, CachePromptGetOutput, error) {
	s.metrics.MCPCallRecorded("cache_prompt_get")
	key, err := semantic.TemplateKey(in.Template, in.VariablesJSON, in.Model)
	if err != nil {
		return nil, CachePromptGetOutput{}, fmt.Errorf("mcp: invalid variables_json: %w", err)
	}
	response, found, cost := s.promptCache.Get(key)
	if found {
		s.metrics.PromptCacheHit()
		if cost > 0 {
			s.analytics.RecordCacheHitSavings("prompt", in.Template, cost)
		}
	} else {
		s.metrics.PromptCacheMiss()
	}
	return nil, CachePromptGetOutput{Found: found, Response: response}, nil
}

// ---- TOOL.CACHE ----

// ToolCacheSetInput is the input for tool_cache_set, mirroring
// TOOL.CACHE SET <tool_name> <args_json> <result> [TTL <seconds>].
type ToolCacheSetInput struct {
	ToolName   string `json:"tool_name" jsonschema:"name of the tool/API call whose result this is"`
	ArgsJSON   string `json:"args_json" jsonschema:"a JSON object of the tool-call arguments; canonicalized (key order doesn't matter) into the cache key"`
	Result     string `json:"result" jsonschema:"the tool-call result to cache"`
	TTLSeconds int    `json:"ttl_seconds,omitempty" jsonschema:"entry lifetime in seconds; omitted or <=0 means the entry never expires"`
}

// ToolCacheSetOutput is the output for tool_cache_set.
type ToolCacheSetOutput struct {
	OK bool `json:"ok"`
}

func (s *Server) toolCacheSet(_ context.Context, _ *sdkmcp.CallToolRequest, in ToolCacheSetInput) (*sdkmcp.CallToolResult, ToolCacheSetOutput, error) {
	s.metrics.MCPCallRecorded("tool_cache_set")
	key, err := toolcache.ToolKey(in.ToolName, in.ArgsJSON)
	if err != nil {
		return nil, ToolCacheSetOutput{}, fmt.Errorf("mcp: invalid args_json: %w", err)
	}
	s.toolCache.Set(key, in.Result, ttlFromSeconds(in.TTLSeconds))
	return nil, ToolCacheSetOutput{OK: true}, nil
}

// ToolCacheGetInput is the input for tool_cache_get, mirroring
// TOOL.CACHE GET <tool_name> <args_json>.
type ToolCacheGetInput struct {
	ToolName string `json:"tool_name" jsonschema:"name of the tool/API call to look up"`
	ArgsJSON string `json:"args_json" jsonschema:"a JSON object of the tool-call arguments; must canonicalize the same way as the original tool_cache_set call"`
}

// ToolCacheGetOutput is the output for tool_cache_get.
type ToolCacheGetOutput struct {
	Found  bool   `json:"found"`
	Result string `json:"result,omitempty"`
}

func (s *Server) toolCacheGet(_ context.Context, _ *sdkmcp.CallToolRequest, in ToolCacheGetInput) (*sdkmcp.CallToolResult, ToolCacheGetOutput, error) {
	s.metrics.MCPCallRecorded("tool_cache_get")
	key, err := toolcache.ToolKey(in.ToolName, in.ArgsJSON)
	if err != nil {
		return nil, ToolCacheGetOutput{}, fmt.Errorf("mcp: invalid args_json: %w", err)
	}
	result, found := s.toolCache.Get(key)
	if found {
		s.metrics.ToolCacheHit()
	} else {
		s.metrics.ToolCacheMiss()
	}
	return nil, ToolCacheGetOutput{Found: found, Result: result}, nil
}

func (s *Server) registerCacheTools() {
	sdkmcp.AddTool(s.sdk, &sdkmcp.Tool{
		Name:        "cache_semantic_set",
		Description: "Store an LLM (prompt, response) pair in Cache-Pot's similarity-based semantic cache, scoped by model and temperature. A later cache_semantic_get with a similar-enough prompt in the same model/temperature partition will return this response instead of requiring an exact match. Backed by the same SemanticCache instance as the CACHE.SEMANTIC SET RESP command.",
	}, s.cacheSemanticSet)

	sdkmcp.AddTool(s.sdk, &sdkmcp.Tool{
		Name:        "cache_semantic_get",
		Description: "Look up a prompt in Cache-Pot's similarity-based semantic cache: finds the closest previously-cached prompt in the same model/temperature partition and returns its cached response if the cosine similarity is at or above the threshold. Backed by the same SemanticCache instance as the CACHE.SEMANTIC GET RESP command.",
	}, s.cacheSemanticGet)

	sdkmcp.AddTool(s.sdk, &sdkmcp.Tool{
		Name:        "cache_prompt_set",
		Description: "Store an exact-match cache entry for a rendered prompt template: the cache key is derived from the template text, its JSON variables, and the model name, so any later call with the exact same template+variables+model is a hit. Backed by the same PromptCache instance as the CACHE.PROMPT SET RESP command.",
	}, s.cachePromptSet)

	sdkmcp.AddTool(s.sdk, &sdkmcp.Tool{
		Name:        "cache_prompt_get",
		Description: "Look up the exact-match prompt-template cache for a given template+variables+model. Backed by the same PromptCache instance as the CACHE.PROMPT GET RESP command.",
	}, s.cachePromptGet)

	sdkmcp.AddTool(s.sdk, &sdkmcp.Tool{
		Name:        "tool_cache_set",
		Description: "Store the result of an agent tool call (e.g. a GitHub/Slack/Jira API call), keyed by tool name and its JSON arguments, so identical future tool calls can be served from cache instead of re-invoking the tool. Backed by the same ToolCache instance as the TOOL.CACHE SET RESP command.",
	}, s.toolCacheSet)

	sdkmcp.AddTool(s.sdk, &sdkmcp.Tool{
		Name:        "tool_cache_get",
		Description: "Look up a previously cached agent tool-call result by tool name and JSON arguments. Backed by the same ToolCache instance as the TOOL.CACHE GET RESP command.",
	}, s.toolCacheGet)
}

// ---- VECTOR.* ----

// StoreVectorInput is the input for store_vector, mirroring
// VECTOR.UPSERT <namespace> <id> <vector_json> [METADATA <metadata_json>]
// [TEXT <text>].
type StoreVectorInput struct {
	Namespace string            `json:"namespace" jsonschema:"namespace/collection this vector belongs to; unrelated namespaces never cross-match"`
	ID        string            `json:"id" jsonschema:"unique id for this vector within namespace; upserting an existing id fully replaces its previous vector/metadata/text"`
	Vector    []float32         `json:"vector" jsonschema:"the embedding to store"`
	Metadata  map[string]string `json:"metadata,omitempty" jsonschema:"optional string-valued metadata usable for exact-match FILTER-ing in find_similar"`
	Text      string            `json:"text,omitempty" jsonschema:"optional raw text payload, only used for hybrid keyword+vector search"`
}

// StoreVectorOutput is the output for store_vector.
type StoreVectorOutput struct {
	OK bool `json:"ok"`
}

func (s *Server) storeVector(_ context.Context, _ *sdkmcp.CallToolRequest, in StoreVectorInput) (*sdkmcp.CallToolResult, StoreVectorOutput, error) {
	s.metrics.MCPCallRecorded("store_vector")
	s.vectorStore.Upsert(in.Namespace, in.ID, in.Vector, in.Metadata, in.Text)
	return nil, StoreVectorOutput{OK: true}, nil
}

// FindSimilarInput is the input for find_similar, mirroring
// VECTOR.SEARCH <namespace> <vector_json> [K <n>] [METRIC ...] [FILTER ...].
// Hybrid keyword+vector search (VECTOR.SEARCH's HYBRID option) is not
// exposed here; this tool covers pure vector search only.
type FindSimilarInput struct {
	Namespace string            `json:"namespace" jsonschema:"namespace/collection to search within"`
	Vector    []float32         `json:"vector" jsonschema:"query embedding"`
	K         *int              `json:"k,omitempty" jsonschema:"max number of nearest neighbors to return; defaults to 10 if omitted"`
	Metric    string            `json:"metric,omitempty" jsonschema:"distance metric: \"cosine\" (default), \"dot\", or \"euclidean\""`
	Filter    map[string]string `json:"filter,omitempty" jsonschema:"optional metadata exact-match filter: only vectors whose metadata matches every key/value pair are considered"`
}

// FindSimilarMatch is one nearest-neighbor result.
type FindSimilarMatch struct {
	ID    string  `json:"id"`
	Score float64 `json:"score"`
}

// FindSimilarOutput is the output for find_similar.
type FindSimilarOutput struct {
	Results []FindSimilarMatch `json:"results"`
}

func (s *Server) findSimilar(_ context.Context, _ *sdkmcp.CallToolRequest, in FindSimilarInput) (*sdkmcp.CallToolResult, FindSimilarOutput, error) {
	s.metrics.MCPCallRecorded("find_similar")
	metric, err := parseMetric(in.Metric)
	if err != nil {
		return nil, FindSimilarOutput{}, err
	}
	k := defaultSearchK
	if in.K != nil {
		k = *in.K
	}

	results := s.vectorStore.Search(in.Namespace, in.Vector, k, metric, in.Filter, nil)
	s.metrics.VectorSearchPerformed()
	out := make([]FindSimilarMatch, len(results))
	for i, r := range results {
		out[i] = FindSimilarMatch{ID: r.ID, Score: r.Score}
	}
	return nil, FindSimilarOutput{Results: out}, nil
}

// DeleteVectorInput is the input for delete_vector, mirroring
// VECTOR.DELETE <namespace> <id>.
type DeleteVectorInput struct {
	Namespace string `json:"namespace" jsonschema:"namespace/collection the vector belongs to"`
	ID        string `json:"id" jsonschema:"id of the vector to remove"`
}

// DeleteVectorOutput is the output for delete_vector.
type DeleteVectorOutput struct {
	Deleted bool `json:"deleted"`
}

func (s *Server) deleteVector(_ context.Context, _ *sdkmcp.CallToolRequest, in DeleteVectorInput) (*sdkmcp.CallToolResult, DeleteVectorOutput, error) {
	s.metrics.MCPCallRecorded("delete_vector")
	deleted := s.vectorStore.Delete(in.Namespace, in.ID)
	return nil, DeleteVectorOutput{Deleted: deleted}, nil
}

func (s *Server) registerVectorTools() {
	sdkmcp.AddTool(s.sdk, &sdkmcp.Tool{
		Name:        "store_vector",
		Description: "Insert or replace an embedding vector under (namespace, id) in Cache-Pot's native vector store. Upserting an existing id fully replaces its previous vector/metadata/text. Backed by the same vector.Store instance as the VECTOR.UPSERT RESP command.",
	}, s.storeVector)

	sdkmcp.AddTool(s.sdk, &sdkmcp.Tool{
		Name:        "find_similar",
		Description: "Find the k nearest-neighbor vectors to a query embedding within a namespace, optionally restricted to entries matching a metadata filter. Backed by the same vector.Store instance as the VECTOR.SEARCH RESP command (pure vector search; hybrid keyword+vector search is not exposed by this tool).",
	}, s.findSimilar)

	sdkmcp.AddTool(s.sdk, &sdkmcp.Tool{
		Name:        "delete_vector",
		Description: "Remove the vector stored under (namespace, id), reporting whether it existed. Backed by the same vector.Store instance as the VECTOR.DELETE RESP command.",
	}, s.deleteVector)
}

// ---- AGENT.REMEMBER / AGENT.RECALL ----

// RememberInput is the input for remember, mirroring
// AGENT.REMEMBER <agent_id> <content> [WORKSPACE <workspace>]
// [KIND short_term|long_term|episodic|semantic] [METADATA <metadata_json>]
// [TTL <seconds>]. Like AGENT.REMEMBER, there is no explicit-ID option:
// remember always generates a fresh memory id rather than supporting
// upsert-by-id (MEMORY.PUT's ID option, not exposed as an MCP tool here).
type RememberInput struct {
	AgentID string `json:"agent_id" jsonschema:"id of the agent this memory belongs to"`
	Content string `json:"content" jsonschema:"the text content to remember"`
	// Workspace and Kind default to "default" and "long_term" respectively
	// when empty, matching AGENT.REMEMBER's defaults when WORKSPACE/KIND
	// are omitted.
	Workspace  string            `json:"workspace,omitempty" jsonschema:"workspace this memory belongs to; defaults to \"default\" if omitted or empty"`
	Kind       string            `json:"kind,omitempty" jsonschema:"memory kind: \"short_term\", \"long_term\" (default), \"episodic\", or \"semantic\""`
	Metadata   map[string]string `json:"metadata,omitempty" jsonschema:"optional string-valued metadata to store alongside this memory"`
	TTLSeconds int               `json:"ttl_seconds,omitempty" jsonschema:"entry lifetime in seconds; omitted or <=0 means the memory never expires"`
}

// RememberOutput is the output for remember.
type RememberOutput struct {
	ID string `json:"id"`
}

func (s *Server) remember(ctx context.Context, _ *sdkmcp.CallToolRequest, in RememberInput) (*sdkmcp.CallToolResult, RememberOutput, error) {
	s.metrics.MCPCallRecorded("remember")
	workspace := in.Workspace
	if workspace == "" {
		workspace = defaultMemoryWorkspace
	}
	kind := defaultMemoryKind
	if in.Kind != "" {
		k, ok := memory.ParseKind(in.Kind)
		if !ok {
			return nil, RememberOutput{}, fmt.Errorf("mcp: unknown kind %q (want \"short_term\", \"long_term\", \"episodic\", or \"semantic\")", in.Kind)
		}
		kind = k
	}

	m := memory.Memory{
		AgentID:     in.AgentID,
		WorkspaceID: workspace,
		Kind:        kind,
		Content:     in.Content,
		Metadata:    in.Metadata,
	}

	id, err := s.memoryStore.Put(ctx, m, ttlFromSeconds(in.TTLSeconds))
	if err != nil {
		return nil, RememberOutput{}, err
	}
	s.metrics.MemoryWrite()
	return nil, RememberOutput{ID: id}, nil
}

// RecallInput is the input for recall, mirroring
// AGENT.RECALL <agent_id> <query> [WORKSPACE <workspace>] [KIND <kind>]
// [K <n>] [THRESHOLD <float>]. agent_id is always applied as the AGENT
// filter, same as AGENT.RECALL -- this tool can only surface the given
// agent's own memories, never another agent's.
type RecallInput struct {
	AgentID   string   `json:"agent_id" jsonschema:"id of the agent whose own memories to search -- results are always scoped to this agent"`
	Query     string   `json:"query" jsonschema:"free-text query to rank memories against by semantic similarity"`
	Workspace string   `json:"workspace,omitempty" jsonschema:"workspace to search within; defaults to \"default\" if omitted or empty"`
	Kind      string   `json:"kind,omitempty" jsonschema:"optional memory kind filter: \"short_term\", \"long_term\", \"episodic\", or \"semantic\""`
	K         int      `json:"k,omitempty" jsonschema:"max number of results to return; defaults to 10 if omitted"`
	Threshold *float64 `json:"threshold,omitempty" jsonschema:"optional minimum cosine similarity in [-1,1] a result must meet; omitted means no minimum (best K matches regardless of score)"`
}

// RecallMatch is one recalled memory's id and similarity score.
type RecallMatch struct {
	ID    string  `json:"id"`
	Score float64 `json:"score"`
}

// RecallOutput is the output for recall.
type RecallOutput struct {
	Results []RecallMatch `json:"results"`
}

func (s *Server) recall(ctx context.Context, _ *sdkmcp.CallToolRequest, in RecallInput) (*sdkmcp.CallToolResult, RecallOutput, error) {
	s.metrics.MCPCallRecorded("recall")
	workspace := in.Workspace
	if workspace == "" {
		workspace = defaultMemoryWorkspace
	}
	var kind *memory.Kind
	if in.Kind != "" {
		k, ok := memory.ParseKind(in.Kind)
		if !ok {
			return nil, RecallOutput{}, fmt.Errorf("mcp: unknown kind %q (want \"short_term\", \"long_term\", \"episodic\", or \"semantic\")", in.Kind)
		}
		kind = &k
	}
	k := defaultMemorySearchK
	if in.K != 0 {
		k = in.K
	}

	results, err := s.memoryStore.Search(ctx, workspace, in.Query, memory.SearchOptions{
		AgentID:   in.AgentID,
		Kind:      kind,
		K:         k,
		Threshold: in.Threshold,
	})
	if err != nil {
		return nil, RecallOutput{}, err
	}
	s.metrics.MemoryRead()
	out := make([]RecallMatch, len(results))
	for i, r := range results {
		out[i] = RecallMatch{ID: r.ID, Score: r.Score}
	}
	return nil, RecallOutput{Results: out}, nil
}

func (s *Server) registerMemoryTools() {
	sdkmcp.AddTool(s.sdk, &sdkmcp.Tool{
		Name:        "remember",
		Description: "Store a memory for this agent, retrievable later by meaning via recall. Backed by the same memory.Store instance as the AGENT.REMEMBER RESP command; always generates a fresh memory id.",
	}, s.remember)

	sdkmcp.AddTool(s.sdk, &sdkmcp.Tool{
		Name:        "recall",
		Description: "Recall this agent's own past memories relevant to a query, ranked by semantic similarity. Never returns another agent's memories. Backed by the same memory.Store instance as the AGENT.RECALL RESP command.",
	}, s.recall)
}

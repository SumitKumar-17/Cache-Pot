package server

// Config holds the Phase 1 server configuration. cmd/cachepotd builds one of
// these from CLI flags with environment-variable fallback and passes it to
// Run.
type Config struct {
	// Port is the TCP port the RESP listener binds to.
	Port int
	// Password, if non-empty, must be supplied via AUTH before other
	// commands are allowed (matches Redis's classic requirepass). Empty
	// means no authentication is required, matching Redis's default.
	Password string
	// MaxConnections bounds the number of concurrent client connections;
	// connections beyond this are rejected with a clean error reply and
	// the socket is closed, rather than being allowed to degrade service.
	MaxConnections int

	// EmbedProvider selects the text-embedding backend that powers
	// CACHE.SEMANTIC: "mock" (default) or "openai".
	//
	// "mock" uses internal/embed.NewMock, a deterministic,
	// dependency-free provider intended for local dev/testing only — it
	// does NOT produce semantically meaningful embeddings, only
	// deterministic ones suitable for exercising the cache's plumbing
	// (exact-duplicate hits, near-duplicate hits, unrelated misses). It
	// is not suitable for production use.
	//
	// "openai" uses internal/embed.NewOpenAI against OpenAI's real
	// embeddings API and requires OpenAIAPIKey to be set.
	EmbedProvider string
	// OpenAIAPIKey is the API key used when EmbedProvider == "openai". It
	// is an error to select "openai" without providing this.
	OpenAIAPIKey string
	// OpenAIAPIBase overrides the base URL used when EmbedProvider ==
	// "openai" (default "https://api.openai.com/v1"). Set this to point
	// at an OpenAI-compatible endpoint (an Azure OpenAI deployment, a
	// self-hosted gateway, etc.) instead of OpenAI's own API.
	OpenAIAPIBase string

	// MCPPort is the TCP port the native MCP (Model Context Protocol) HTTP
	// server listens on, exposing the same SemanticCache/PromptCache/
	// ToolCache/VectorStore instances the RESP listener uses as MCP tools
	// (see internal/mcp). Set to 0 to disable the MCP listener entirely.
	MCPPort int

	// MaxEntries bounds the total number of live keys (server-wide, across
	// all workspaces) memstore.Store will hold before evicting to make
	// room for a new key. 0 means unlimited (eviction disabled), matching
	// this project's "0 means off" convention (e.g. MCPPort).
	MaxEntries int
	// EvictionPolicy selects the eviction.Policy used once MaxEntries is
	// exceeded: "lru" (default) or "weighted". Any other value is a
	// startup error.
	EvictionPolicy string
}

const (
	// DefaultPort is deliberately not 6379, Redis's default, so cachepotd
	// doesn't collide with a real local Redis during development/testing.
	DefaultPort           = 6380
	DefaultMaxConnections = 10000
	// DefaultEmbedProvider is "mock" so cachepotd runs out of the box
	// with no external dependencies/API keys required.
	DefaultEmbedProvider = "mock"
	// DefaultMCPPort is deliberately not a well-known port either, and
	// sits right after DefaultPort so the two listeners are easy to
	// remember together. Set --mcp-port/CACHEPOT_MCP_PORT to 0 to disable
	// the MCP listener entirely.
	DefaultMCPPort = 6381
	// DefaultMaxEntries is 0 (unlimited) so cachepotd behaves exactly as it
	// always has -- no memory/entry-count bound -- unless an operator
	// explicitly opts in.
	DefaultMaxEntries = 0
	// DefaultEvictionPolicy is "lru", matching this project's original
	// (and only, prior to Phase 5) eviction policy.
	DefaultEvictionPolicy = "lru"
)

// DefaultConfig returns the Phase 1 default configuration (no auth
// required).
func DefaultConfig() Config {
	return Config{
		Port:           DefaultPort,
		MaxConnections: DefaultMaxConnections,
		EmbedProvider:  DefaultEmbedProvider,
		MCPPort:        DefaultMCPPort,
		MaxEntries:     DefaultMaxEntries,
		EvictionPolicy: DefaultEvictionPolicy,
	}
}

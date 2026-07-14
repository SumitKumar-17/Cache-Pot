// Command cachepotd is the Cache-Pot server entrypoint: it parses
// flags/environment variables into a server.Config and runs the RESP
// server until interrupted.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/SumitKumar-17/cache-pot/internal/auth"
	"github.com/SumitKumar-17/cache-pot/internal/server"
)

func main() {
	loadDotEnv(".env")
	cfg, err := parseConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, "cachepotd:", err)
		os.Exit(1)
	}
	if err := server.Run(context.Background(), cfg); err != nil {
		fmt.Fprintln(os.Stderr, "cachepotd:", err)
		os.Exit(1)
	}
}

// loadDotEnv reads simple KEY=VALUE lines from path (if it exists) and sets
// them as process environment variables, without overriding any variable
// already present in the real environment — a real env var always wins,
// matching standard precedence for this kind of convenience loader. This is
// a deliberately minimal parser (no multi-line values, no variable
// expansion) rather than a third-party dependency: it exists purely so
// `cachepotd` picks up a local .env file (e.g. OPENAI_API_KEY,
// OPENAI_API_BASE) without requiring the caller to export them by hand.
// Missing or unreadable files are silently ignored.
func loadDotEnv(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		value = strings.Trim(value, `"'`)
		if key == "" {
			continue
		}
		if _, alreadySet := os.LookupEnv(key); !alreadySet {
			os.Setenv(key, value)
		}
	}
}

// parseWorkspaceCredentials parses a comma-separated "workspace:password"
// list (e.g. "acme:secret1,other:secret2") into []auth.Credential, matching
// --workspace-credentials/CACHEPOT_WORKSPACE_CREDENTIALS's documented
// format. An empty input returns a nil slice (no error) since this is an
// opt-in feature -- most deployments won't set it at all. A malformed entry
// (missing the ':' separator, or an empty workspace/password on either
// side of it) is a startup error with a clear message, matching
// buildEmbedProvider/buildCompletionProvider/buildEvictionPolicy's
// fail-loudly convention in internal/server/server.go.
func parseWorkspaceCredentials(s string) ([]auth.Credential, error) {
	if s == "" {
		return nil, nil
	}
	parts := strings.Split(s, ",")
	creds := make([]auth.Credential, 0, len(parts))
	for _, part := range parts {
		workspace, password, ok := strings.Cut(part, ":")
		if !ok || workspace == "" || password == "" {
			return nil, fmt.Errorf("invalid --workspace-credentials entry %q: want \"workspace:password\"", part)
		}
		creds = append(creds, auth.Credential{Workspace: workspace, Password: password})
	}
	return creds, nil
}

// parseConfig builds a server.Config from CLI flags, falling back to
// environment variables, falling back to hard-coded defaults. Flags always
// win when explicitly passed, since each flag's default is itself the
// environment-variable value (or the hard-coded default if the env var is
// unset/unparseable).
func parseConfig() (server.Config, error) {
	envPort := server.DefaultPort
	if v := os.Getenv("CACHEPOT_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			envPort = n
		}
	}
	envPassword := os.Getenv("CACHEPOT_PASSWORD")
	envWorkspaceCredentials := os.Getenv("CACHEPOT_WORKSPACE_CREDENTIALS")
	envMaxConns := server.DefaultMaxConnections
	if v := os.Getenv("CACHEPOT_MAX_CONNECTIONS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			envMaxConns = n
		}
	}
	envEmbedProvider := server.DefaultEmbedProvider
	if v := os.Getenv("CACHEPOT_EMBED_PROVIDER"); v != "" {
		envEmbedProvider = v
	}
	envOpenAIAPIKey := os.Getenv("OPENAI_API_KEY")
	envOpenAIAPIBase := os.Getenv("OPENAI_API_BASE")
	envCompletionProvider := server.DefaultCompletionProvider
	if v := os.Getenv("CACHEPOT_COMPLETION_PROVIDER"); v != "" {
		envCompletionProvider = v
	}
	envOpenAICompletionModel := server.DefaultOpenAICompletionModel
	if v := os.Getenv("OPENAI_COMPLETION_MODEL"); v != "" {
		envOpenAICompletionModel = v
	}
	envMCPPort := server.DefaultMCPPort
	if v := os.Getenv("CACHEPOT_MCP_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			envMCPPort = n
		}
	}
	envMaxEntries := server.DefaultMaxEntries
	if v := os.Getenv("CACHEPOT_MAX_ENTRIES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			envMaxEntries = n
		}
	}
	envEvictionPolicy := server.DefaultEvictionPolicy
	if v := os.Getenv("CACHEPOT_EVICTION_POLICY"); v != "" {
		envEvictionPolicy = v
	}

	// password and openAIAPIKey are secrets: their flag defaults are left
	// empty (NOT envPassword/envOpenAIAPIKey) so `--help` never prints a
	// real credential pulled from the environment or a loaded .env file —
	// flag's help output always includes each flag's default value, and a
	// secret default would leak into terminal scrollback, screenshots, CI
	// logs, etc. The env-var fallback is applied manually after
	// flag.Parse() instead, preserving the usual flag > env > default
	// precedence without ever making the secret visible in --help.
	port := flag.Int("port", envPort, "TCP port to listen on (env CACHEPOT_PORT)")
	password := flag.String("password", "", "required AUTH password; empty means no auth (env CACHEPOT_PASSWORD)")
	workspaceCredentials := flag.String("workspace-credentials", "", `comma-separated "workspace:password" pairs enabling multi-workspace AUTH mode (e.g. "acme:secret1,other:secret2"); mutually exclusive with --password (env CACHEPOT_WORKSPACE_CREDENTIALS)`)
	maxConns := flag.Int("max-connections", envMaxConns, "maximum concurrent client connections (env CACHEPOT_MAX_CONNECTIONS)")
	embedProvider := flag.String("embed-provider", envEmbedProvider, `text-embedding backend for CACHE.SEMANTIC: "mock" or "openai" (env CACHEPOT_EMBED_PROVIDER)`)
	openAIAPIKey := flag.String("openai-api-key", "", "OpenAI API key, required when --embed-provider=openai (env OPENAI_API_KEY)")
	openAIAPIBase := flag.String("openai-api-base", envOpenAIAPIBase, `OpenAI-compatible API base URL, defaults to OpenAI's own API (env OPENAI_API_BASE)`)
	completionProvider := flag.String("completion-provider", envCompletionProvider, `text-generation (chat completion) backend for consolidation/graph-extraction features: "mock" or "openai" (env CACHEPOT_COMPLETION_PROVIDER)`)
	openAICompletionModel := flag.String("openai-completion-model", envOpenAICompletionModel, `OpenAI chat-completion model, used when --completion-provider=openai (env OPENAI_COMPLETION_MODEL)`)
	mcpPort := flag.Int("mcp-port", envMCPPort, "TCP port for the native MCP (Model Context Protocol) HTTP server; 0 disables it (env CACHEPOT_MCP_PORT)")
	maxEntries := flag.Int("max-entries", envMaxEntries, "maximum total live keys before eviction kicks in; 0 means unlimited (env CACHEPOT_MAX_ENTRIES)")
	evictionPolicy := flag.String("eviction-policy", envEvictionPolicy, `eviction policy used once --max-entries is exceeded: "lru" or "weighted" (env CACHEPOT_EVICTION_POLICY)`)
	flag.Parse()

	resolvedPassword := *password
	if resolvedPassword == "" {
		resolvedPassword = envPassword
	}
	resolvedOpenAIAPIKey := *openAIAPIKey
	if resolvedOpenAIAPIKey == "" {
		resolvedOpenAIAPIKey = envOpenAIAPIKey
	}
	resolvedWorkspaceCredentials := *workspaceCredentials
	if resolvedWorkspaceCredentials == "" {
		resolvedWorkspaceCredentials = envWorkspaceCredentials
	}
	workspaceCreds, err := parseWorkspaceCredentials(resolvedWorkspaceCredentials)
	if err != nil {
		return server.Config{}, err
	}

	return server.Config{
		Port:                  *port,
		Password:              resolvedPassword,
		MaxConnections:        *maxConns,
		EmbedProvider:         *embedProvider,
		OpenAIAPIKey:          resolvedOpenAIAPIKey,
		OpenAIAPIBase:         *openAIAPIBase,
		CompletionProvider:    *completionProvider,
		OpenAICompletionModel: *openAICompletionModel,
		MCPPort:               *mcpPort,
		MaxEntries:            *maxEntries,
		EvictionPolicy:        *evictionPolicy,
		WorkspaceCredentials:  workspaceCreds,
	}, nil
}

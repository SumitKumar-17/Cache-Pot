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

	"github.com/SumitKumar-17/cache-pot/internal/server"
)

func main() {
	loadDotEnv(".env")
	cfg := parseConfig()
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

// parseConfig builds a server.Config from CLI flags, falling back to
// environment variables, falling back to hard-coded defaults. Flags always
// win when explicitly passed, since each flag's default is itself the
// environment-variable value (or the hard-coded default if the env var is
// unset/unparseable).
func parseConfig() server.Config {
	envPort := server.DefaultPort
	if v := os.Getenv("CACHEPOT_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			envPort = n
		}
	}
	envPassword := os.Getenv("CACHEPOT_PASSWORD")
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
	envMCPPort := server.DefaultMCPPort
	if v := os.Getenv("CACHEPOT_MCP_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			envMCPPort = n
		}
	}

	port := flag.Int("port", envPort, "TCP port to listen on (env CACHEPOT_PORT)")
	password := flag.String("password", envPassword, "required AUTH password; empty means no auth (env CACHEPOT_PASSWORD)")
	maxConns := flag.Int("max-connections", envMaxConns, "maximum concurrent client connections (env CACHEPOT_MAX_CONNECTIONS)")
	embedProvider := flag.String("embed-provider", envEmbedProvider, `text-embedding backend for CACHE.SEMANTIC: "mock" or "openai" (env CACHEPOT_EMBED_PROVIDER)`)
	openAIAPIKey := flag.String("openai-api-key", envOpenAIAPIKey, "OpenAI API key, required when --embed-provider=openai (env OPENAI_API_KEY)")
	openAIAPIBase := flag.String("openai-api-base", envOpenAIAPIBase, `OpenAI-compatible API base URL, defaults to OpenAI's own API (env OPENAI_API_BASE)`)
	mcpPort := flag.Int("mcp-port", envMCPPort, "TCP port for the native MCP (Model Context Protocol) HTTP server; 0 disables it (env CACHEPOT_MCP_PORT)")
	flag.Parse()

	return server.Config{
		Port:           *port,
		Password:       *password,
		MaxConnections: *maxConns,
		EmbedProvider:  *embedProvider,
		OpenAIAPIKey:   *openAIAPIKey,
		OpenAIAPIBase:  *openAIAPIBase,
		MCPPort:        *mcpPort,
	}
}

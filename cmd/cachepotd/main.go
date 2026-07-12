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

	"github.com/SumitKumar-17/cache-pot/internal/server"
)

func main() {
	cfg := parseConfig()
	if err := server.Run(context.Background(), cfg); err != nil {
		fmt.Fprintln(os.Stderr, "cachepotd:", err)
		os.Exit(1)
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

	port := flag.Int("port", envPort, "TCP port to listen on (env CACHEPOT_PORT)")
	password := flag.String("password", envPassword, "required AUTH password; empty means no auth (env CACHEPOT_PASSWORD)")
	maxConns := flag.Int("max-connections", envMaxConns, "maximum concurrent client connections (env CACHEPOT_MAX_CONNECTIONS)")
	embedProvider := flag.String("embed-provider", envEmbedProvider, `text-embedding backend for CACHE.SEMANTIC: "mock" or "openai" (env CACHEPOT_EMBED_PROVIDER)`)
	openAIAPIKey := flag.String("openai-api-key", envOpenAIAPIKey, "OpenAI API key, required when --embed-provider=openai (env OPENAI_API_KEY)")
	flag.Parse()

	return server.Config{
		Port:           *port,
		Password:       *password,
		MaxConnections: *maxConns,
		EmbedProvider:  *embedProvider,
		OpenAIAPIKey:   *openAIAPIKey,
	}
}

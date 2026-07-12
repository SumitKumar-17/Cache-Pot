// Package server wires together storage, auth, observability, and the RESP
// protocol layer into a runnable Cache-Pot process.
package server

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/SumitKumar-17/cache-pot/internal/auth"
	"github.com/SumitKumar-17/cache-pot/internal/embed"
	"github.com/SumitKumar-17/cache-pot/internal/observability"
	"github.com/SumitKumar-17/cache-pot/internal/semantic"
	"github.com/SumitKumar-17/cache-pot/internal/server/resp"
	"github.com/SumitKumar-17/cache-pot/internal/storage/memstore"
	"github.com/SumitKumar-17/cache-pot/internal/toolcache"
	"github.com/SumitKumar-17/cache-pot/internal/vector"
)

// shutdownGrace is how long Run waits for in-flight connections to finish
// on their own after the listener is closed, before returning anyway.
const shutdownGrace = 5 * time.Second

// Server owns the RESP listener and its lifecycle: accepting connections
// (subject to Config.MaxConnections), and shutting down gracefully.
type Server struct {
	cfg     Config
	logger  *slog.Logger
	metrics *observability.Metrics
	deps    *resp.Deps
}

// Run builds a Server from cfg and runs it until ctx is canceled or the
// process receives SIGINT/SIGTERM, then shuts down gracefully: the listener
// stops accepting immediately, and in-flight connections get up to
// shutdownGrace to finish before Run returns.
func Run(ctx context.Context, cfg Config) error {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.Port))
	if err != nil {
		return fmt.Errorf("server: listen on port %d: %w", cfg.Port, err)
	}
	return RunListener(ctx, cfg, ln)
}

// RunListener is like Run but accepts an already-created listener instead
// of binding cfg.Port itself. This exists primarily so tests (see
// test/integration) can bind a random free port via net.Listen(":0") and
// hand the listener straight to the server, with no race between picking a
// port and the server binding it.
func RunListener(ctx context.Context, cfg Config, ln net.Listener) error {
	s := &Server{
		cfg:     cfg,
		logger:  observability.NewLogger(slog.LevelInfo),
		metrics: observability.NewMetrics(),
	}
	return s.run(ctx, ln)
}

func (s *Server) run(ctx context.Context, ln net.Listener) error {
	engine := memstore.New(32)
	defer engine.Close()

	provider, err := buildEmbedProvider(s.cfg)
	if err != nil {
		return err
	}

	registry := resp.NewRegistry()
	resp.RegisterAll(registry)

	s.deps = &resp.Deps{
		Engine:        engine,
		Auth:          auth.New(s.cfg.Password),
		Metrics:       s.metrics,
		Logger:        s.logger,
		PubSub:        resp.NewPubSub(),
		Registry:      registry,
		SemanticCache: semantic.New(provider),
		PromptCache:   semantic.NewPromptCache(),
		ToolCache:     toolcache.New(),
		VectorStore:   vector.New(),
	}

	s.logger.Info("cachepot listening", "addr", ln.Addr().String())

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	maxConns := s.cfg.MaxConnections
	if maxConns <= 0 {
		maxConns = DefaultMaxConnections
	}
	connSlots := make(chan struct{}, maxConns)

	var wg sync.WaitGroup
	var closing atomic.Bool

	go func() {
		<-ctx.Done()
		closing.Store(true)
		_ = ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if closing.Load() {
				break
			}
			s.logger.Error("accept error", "err", err)
			continue
		}

		select {
		case connSlots <- struct{}{}:
		default:
			// MaxConnections reached: reject cleanly rather than letting
			// the server degrade under unbounded goroutine/connection
			// growth.
			s.metrics.ConnectionRejected()
			_, _ = conn.Write([]byte("-ERR max number of clients reached\r\n"))
			_ = conn.Close()
			continue
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-connSlots }()
			resp.HandleConn(conn, s.deps)
		}()
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(shutdownGrace):
		s.logger.Warn("shutdown grace period elapsed with connections still active")
	}

	s.logger.Info("cachepot stopped")
	return nil
}

// buildEmbedProvider constructs the embed.Provider selected by
// cfg.EmbedProvider ("mock" or "openai", case-insensitive; empty defaults
// to "mock"). It returns an error at startup — rather than lazily at first
// use — if "openai" is selected without an API key, or if EmbedProvider
// names anything else, so misconfiguration is loud and immediate.
func buildEmbedProvider(cfg Config) (embed.Provider, error) {
	switch strings.ToLower(cfg.EmbedProvider) {
	case "", "mock":
		// mock is for local dev/testing only: it produces deterministic
		// but NOT semantically meaningful embeddings, sufficient to
		// exercise CACHE.SEMANTIC's matching logic offline but not
		// suitable for production use.
		return embed.NewMock(0), nil
	case "openai":
		if cfg.OpenAIAPIKey == "" {
			return nil, fmt.Errorf("server: embed-provider=openai requires an OpenAI API key (--openai-api-key or OPENAI_API_KEY)")
		}
		return embed.NewOpenAI(cfg.OpenAIAPIKey, "", cfg.OpenAIAPIBase), nil
	default:
		return nil, fmt.Errorf("server: unknown embed provider %q (want \"mock\" or \"openai\")", cfg.EmbedProvider)
	}
}

// Package server wires together storage, auth, observability, and the RESP
// protocol layer into a runnable Cache-Pot process.
package server

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/SumitKumar-17/cache-pot/internal/analytics"
	"github.com/SumitKumar-17/cache-pot/internal/auth"
	"github.com/SumitKumar-17/cache-pot/internal/embed"
	"github.com/SumitKumar-17/cache-pot/internal/eviction"
	"github.com/SumitKumar-17/cache-pot/internal/llm"
	"github.com/SumitKumar-17/cache-pot/internal/mcp"
	"github.com/SumitKumar-17/cache-pot/internal/memory"
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
	cfg       Config
	logger    *slog.Logger
	metrics   *observability.Metrics
	analytics *analytics.Tracker
	deps      *resp.Deps
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
		cfg:       cfg,
		logger:    observability.NewLogger(slog.LevelInfo),
		metrics:   observability.NewMetrics(),
		analytics: analytics.New(),
	}
	return s.run(ctx, ln)
}

func (s *Server) run(ctx context.Context, ln net.Listener) error {
	evictionPolicy, err := buildEvictionPolicy(s.cfg)
	if err != nil {
		return err
	}
	engine := memstore.New(32,
		memstore.WithMaxEntries(s.cfg.MaxEntries),
		memstore.WithEvictionPolicy(evictionPolicy),
		memstore.WithOnEvict(s.metrics.KeyEvicted),
	)
	defer engine.Close()

	provider, err := buildEmbedProvider(s.cfg)
	if err != nil {
		return err
	}
	// Wrap once, here, so SemanticCache and MemoryStore below share the same
	// instrumented instance -- every Embed/EmbedBatch call either makes,
	// regardless of caller, is recorded on s.metrics (see
	// internal/observability's embed_instrument.go). Passing s.analytics
	// here too means that, for providers reporting real token usage (the
	// OpenAI provider), every embedding call's cost is recorded exactly
	// once, regardless of caller.
	provider = observability.InstrumentProvider(provider, s.metrics, s.analytics)

	completionProvider, err := buildCompletionProvider(s.cfg)
	if err != nil {
		return err
	}
	// Wrapped once here, exactly like the embed.Provider above, so every
	// future consumer (Phase 6's consolidation/summarization and
	// knowledge-graph extraction, landing in later commits) shares the
	// same instrumented instance and therefore the same metrics/cost
	// tracking, regardless of how many places end up calling Complete.
	completionProvider = observability.InstrumentCompletionProvider(completionProvider, s.metrics, s.analytics)

	registry := resp.NewRegistry()
	resp.RegisterAll(registry)

	s.deps = &resp.Deps{
		Engine:             engine,
		Auth:               auth.New(s.cfg.Password),
		Metrics:            s.metrics,
		Logger:             s.logger,
		PubSub:             resp.NewPubSub(),
		Registry:           registry,
		SemanticCache:      semantic.New(provider),
		PromptCache:        semantic.NewPromptCache(),
		ToolCache:          toolcache.New(),
		VectorStore:        vector.New(),
		MemoryStore:        memory.New(provider),
		Analytics:          s.analytics,
		CompletionProvider: completionProvider,
	}

	s.logger.Info("cachepot listening", "addr", ln.Addr().String())

	// The MCP HTTP server, if enabled, is started inside this same process
	// and shares s.deps's SemanticCache/PromptCache/ToolCache/VectorStore/
	// MemoryStore instances with the RESP listener above -- an MCP client
	// and a RESP client observe the exact same cache/vector-store/memory
	// state, with no adapter layer or second storage in between.
	//
	// /metrics (Prometheus text), /stats (JSON), and /dashboard (operator
	// HTML) are mounted on the same listener/mux, alongside the MCP
	// handler at "/" -- http.ServeMux (Go 1.22+) prefers a more specific
	// registered pattern like "/metrics" over the catch-all "/", so this
	// doesn't disturb existing MCP client connections at the documented
	// http://host:6381/ address. This does mean /metrics, /stats, and
	// /dashboard are only reachable when the MCP listener itself is
	// enabled (--mcp-port != 0) -- they share its port rather than getting
	// a dedicated one, to avoid adding a second listener/flag for a need
	// that doesn't (yet) justify one. A standalone --metrics-port would be
	// a reasonable future addition if that coupling proves undesirable.
	var mcpSrv *http.Server
	var mcpDone chan struct{}
	if s.cfg.MCPPort != 0 {
		mcpLn, err := net.Listen("tcp", fmt.Sprintf(":%d", s.cfg.MCPPort))
		if err != nil {
			return fmt.Errorf("server: listen on MCP port %d: %w", s.cfg.MCPPort, err)
		}
		mcpServer := mcp.New(s.deps.SemanticCache, s.deps.PromptCache, s.deps.ToolCache, s.deps.VectorStore, s.deps.MemoryStore, s.metrics, s.analytics)
		mux := http.NewServeMux()
		mux.Handle("/", mcpServer.Handler())
		mux.Handle("/metrics", observability.MetricsHandler(s.metrics))
		mux.Handle("/stats", observability.StatsHandler(s.metrics, s.analytics))
		mux.Handle("/dashboard", observability.DashboardHandler(s.metrics, s.analytics))
		mcpSrv = &http.Server{Handler: mux}
		mcpDone = make(chan struct{})
		go func() {
			defer close(mcpDone)
			if err := mcpSrv.Serve(mcpLn); err != nil && err != http.ErrServerClosed {
				s.logger.Error("mcp server error", "err", err)
			}
		}()
		s.logger.Info("cachepot mcp server listening", "addr", mcpLn.Addr().String())
	}

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
		if mcpSrv != nil {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
			defer cancel()
			_ = mcpSrv.Shutdown(shutdownCtx)
		}
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

	if mcpDone != nil {
		select {
		case <-mcpDone:
		case <-time.After(shutdownGrace):
			s.logger.Warn("mcp server shutdown grace period elapsed")
		}
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

// buildCompletionProvider constructs the llm.CompletionProvider selected
// by cfg.CompletionProvider ("mock" or "openai", case-insensitive; empty
// defaults to "mock"), mirroring buildEmbedProvider exactly: it returns an
// error at startup -- rather than lazily at first use -- if "openai" is
// selected without an API key, or if CompletionProvider names anything
// else, so misconfiguration is loud and immediate.
func buildCompletionProvider(cfg Config) (llm.CompletionProvider, error) {
	switch strings.ToLower(cfg.CompletionProvider) {
	case "", "mock":
		// mock is for local dev/testing only: it performs NO real
		// language understanding or generation, only a deterministic
		// echo of its input, sufficient to exercise Phase 6 consumers'
		// plumbing offline but not suitable for production use.
		return llm.NewMock(), nil
	case "openai":
		if cfg.OpenAIAPIKey == "" {
			return nil, fmt.Errorf("server: completion-provider=openai requires an OpenAI API key (--openai-api-key or OPENAI_API_KEY)")
		}
		return llm.NewOpenAI(cfg.OpenAIAPIKey, cfg.OpenAICompletionModel, cfg.OpenAIAPIBase), nil
	default:
		return nil, fmt.Errorf("server: unknown completion provider %q (want \"mock\" or \"openai\")", cfg.CompletionProvider)
	}
}

// buildEvictionPolicy constructs the eviction.Policy selected by
// cfg.EvictionPolicy ("lru" or "weighted", case-insensitive; empty defaults
// to "lru"). Like buildEmbedProvider, it fails loudly at startup on an
// unrecognized value rather than silently defaulting.
func buildEvictionPolicy(cfg Config) (eviction.Policy, error) {
	switch strings.ToLower(cfg.EvictionPolicy) {
	case "", "lru":
		return eviction.NewLRU(), nil
	case "weighted":
		return eviction.NewWeighted(nil), nil
	default:
		return nil, fmt.Errorf("server: unknown eviction policy %q (want \"lru\" or \"weighted\")", cfg.EvictionPolicy)
	}
}

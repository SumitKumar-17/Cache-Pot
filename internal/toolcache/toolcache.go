// Package toolcache is a Phase 2 skeleton: caching for tool-call
// results (e.g. an agent framework's function/tool invocations), keyed by
// tool name + arguments, distinct from the LLM-response semantic cache in
// internal/semantic. No implementation exists yet in Phase 1.
package toolcache

import (
	"context"
	"time"
)

// ToolCache is the Phase 2 seam for TOOL.CACHE style commands. Not
// implemented in Phase 1.
type ToolCache interface {
	Get(ctx context.Context, workspace, toolName, argsHash string) (result []byte, hit bool, err error)
	Put(ctx context.Context, workspace, toolName, argsHash string, result []byte, ttl time.Duration) error
}

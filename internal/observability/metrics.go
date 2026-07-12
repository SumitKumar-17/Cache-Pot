// Package observability provides minimal, dependency-free instrumentation:
// atomic counters plus a thin slog wrapper. It is deliberately structured so
// a Prometheus (or other) exporter can wrap Metrics.Snapshot() later without
// pulling a metrics client library into Phase 1.
package observability

import "sync/atomic"

// Metrics holds process-wide atomic counters. All fields are safe for
// concurrent use from any goroutine (RESP connection handlers, the TTL
// reaper, etc.).
type Metrics struct {
	connectionsTotal    atomic.Int64
	connectionsActive   atomic.Int64
	connectionsRejected atomic.Int64
	commandsTotal       atomic.Int64
	errorsTotal         atomic.Int64
}

// NewMetrics constructs an empty Metrics.
func NewMetrics() *Metrics {
	return &Metrics{}
}

// ConnectionOpened records a new accepted connection.
func (m *Metrics) ConnectionOpened() {
	m.connectionsTotal.Add(1)
	m.connectionsActive.Add(1)
}

// ConnectionClosed records a connection going away.
func (m *Metrics) ConnectionClosed() {
	m.connectionsActive.Add(-1)
}

// ConnectionRejected records a connection refused (e.g. MaxConnections hit).
func (m *Metrics) ConnectionRejected() {
	m.connectionsRejected.Add(1)
}

// CommandExecuted records one dispatched command.
func (m *Metrics) CommandExecuted() {
	m.commandsTotal.Add(1)
}

// ErrorReturned records one error reply sent to a client.
func (m *Metrics) ErrorReturned() {
	m.errorsTotal.Add(1)
}

// Snapshot is a point-in-time copy of counter values, safe to read without
// further synchronization. A Prometheus exporter (or any other sink) can be
// built by periodically calling Metrics.Snapshot() and translating the
// fields into its own metric types.
type Snapshot struct {
	ConnectionsTotal    int64
	ConnectionsActive   int64
	ConnectionsRejected int64
	CommandsTotal       int64
	ErrorsTotal         int64
}

// Snapshot returns the current counter values.
func (m *Metrics) Snapshot() Snapshot {
	return Snapshot{
		ConnectionsTotal:    m.connectionsTotal.Load(),
		ConnectionsActive:   m.connectionsActive.Load(),
		ConnectionsRejected: m.connectionsRejected.Load(),
		CommandsTotal:       m.commandsTotal.Load(),
		ErrorsTotal:         m.errorsTotal.Load(),
	}
}

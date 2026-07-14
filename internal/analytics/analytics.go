// Package analytics is Cache-Pot's cost/savings/token layer: a
// dependency-free, in-memory Tracker that answers "what has this process
// spent on embeddings" and "how much money has caching saved" for an
// operator-facing dashboard.
//
// This is deliberately simple -- a dashboard data source, not a metrics
// warehouse or time-series store, the same spirit as
// internal/observability.Metrics. The two packages stay separate on
// purpose: Metrics owns hit/miss counting and hit-rate math; Tracker owns
// cost, token usage, and money saved. Nothing here duplicates that other
// package's work.
package analytics

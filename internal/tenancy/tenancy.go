// Package tenancy is a Phase 7 skeleton: multi-tenant workspace management.
// internal/storage.Engine and internal/storage/memstore.Entry already carry
// a "workspace" identifier through every call (see storage.Engine's doc
// comment) so this phase can add real per-workspace isolation, quotas, and
// access control without changing those call sites. No implementation
// exists yet in Phase 1.
package tenancy

// Workspace is a tenant's isolated namespace: its own keyspace, its own
// quotas, and (eventually) its own access-control policy.
type Workspace struct {
	ID          string
	DisplayName string
}

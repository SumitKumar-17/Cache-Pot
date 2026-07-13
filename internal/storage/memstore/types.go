package memstore

import "time"

// Kind identifies which field of Entry holds the value.
type Kind byte

const (
	KindNone Kind = iota
	KindString
	KindHash
	KindList
	KindSet
	KindZSet
)

// String returns the Redis TYPE-command name for the kind.
func (k Kind) String() string {
	switch k {
	case KindString:
		return "string"
	case KindHash:
		return "hash"
	case KindList:
		return "list"
	case KindSet:
		return "set"
	case KindZSet:
		return "zset"
	default:
		return "none"
	}
}

// Entry is the single value container stored per key. Exactly one of the
// type-specific fields is populated, selected by Kind. ExpiresAt is a
// pointer so "no TTL" (nil) is distinguishable from "expires at the zero
// time" without a second parallel TTL map/index.
type Entry struct {
	Kind Kind

	StringVal []byte
	Hash      map[string][]byte
	List      [][]byte
	Set       map[string]struct{}
	ZSet      map[string]float64

	ExpiresAt *time.Time

	// LastAccess supports the Phase 1 LRU eviction policy
	// (internal/eviction/lru.go). Updated on reads and writes.
	LastAccess time.Time

	// AccessCount is the number of times this entry has been read/written
	// (incremented at every call site that also updates LastAccess). It
	// feeds internal/eviction.Weighted's frequency signal; 0 means "never
	// tracked as accessed beyond creation," not "explicitly cold."
	AccessCount int64
}

// expired reports whether the entry's TTL has passed as of now.
func (e *Entry) expired(now time.Time) bool {
	return e.ExpiresAt != nil && !e.ExpiresAt.After(now)
}

// Package storage defines the Engine interface: the single seam between the
// RESP protocol layer (internal/server/resp) and any concrete data-structure
// store. Phase 1 ships one implementation, internal/storage/memstore, but the
// interface is designed so future phases (semantic cache, vector index,
// tiered/remote storage, etc.) can plug in alternate engines, or wrap this one
// with additional behavior, without changing the command dispatch layer.
package storage

import "time"

// defaultWorkspace-style multi-tenancy note:
//
// Every Engine method takes a workspace string as its first parameter.
// memstore namespaces every key by (workspace, key) internally (see
// memstore.nsKey), so this was never just an inert placeholder — Phase 7
// built real per-workspace AUTH enforcement (internal/auth,
// ClientState.authorizedForWorkspace) on top of this existing routing,
// without changing a single storage call site.

// SetOpts carries the optional modifiers for the SET command.
type SetOpts struct {
	TTL      time.Duration // 0 = no expiry
	OnlyIfNX bool          // only set if key does not already exist
	OnlyIfXX bool          // only set if key already exists
	GetOld   bool          // return the previous value (SET ... GET)
}

// ZMember is a single member/score pair returned by sorted-set range queries.
type ZMember struct {
	Member string
	Score  float64
}

// Engine is the storage seam implemented by internal/storage/memstore.Store
// (Phase 1) and, in later phases, potentially by other backends. It exposes
// every data-structure operation the RESP command handlers need, keyed by
// workspace + key.
type Engine interface {
	// Get and Set return err = ErrWrongType (wrapped) when the key holds a
	// non-string value. This is the one deviation from a literal (val, ok)
	// surface: GET on a hash/list/set/zset key must produce a Redis
	// WRONGTYPE error, not a silent "not found", and SET ... GET must do
	// the same when returning a non-string previous value.
	Get(workspace, key string) (val []byte, ok bool, err error)
	Set(workspace, key string, val []byte, opts SetOpts) (ok bool, prevVal []byte, hadPrev bool, err error)
	Del(workspace string, keys ...string) (deleted int)
	Exists(workspace string, keys ...string) (count int)
	Expire(workspace, key string, ttl time.Duration) bool
	TTL(workspace, key string) (ttl time.Duration, hasTTL bool, exists bool)
	Persist(workspace, key string) bool
	Type(workspace, key string) (string, bool)
	Rename(workspace, oldKey, newKey string) bool
	Keys(workspace, pattern string) []string
	Scan(workspace string, cursor uint64, match string, count int) (nextCursor uint64, keys []string)
	FlushDB(workspace string)

	// hash
	HSet(workspace, key string, fields map[string][]byte) (added int, err error)
	HGet(workspace, key, field string) ([]byte, bool, error)
	HGetAll(workspace, key string) (map[string][]byte, bool, error)
	HDel(workspace, key string, fields ...string) (int, error)
	HExists(workspace, key, field string) (bool, error)
	HKeys(workspace, key string) ([]string, bool, error)
	HVals(workspace, key string) ([][]byte, bool, error)
	HLen(workspace, key string) (int, error)
	HMGet(workspace, key string, fields ...string) ([][]byte, error)
	HIncrBy(workspace, key, field string, delta int64) (int64, error)

	// list
	LPush(workspace, key string, vals ...[]byte) (int, error)
	RPush(workspace, key string, vals ...[]byte) (int, error)
	LPop(workspace, key string) ([]byte, bool, error)
	RPop(workspace, key string) ([]byte, bool, error)
	LRange(workspace, key string, start, stop int) ([][]byte, error)
	LLen(workspace, key string) (int, error)
	LIndex(workspace, key string, index int) ([]byte, bool, error)
	LSet(workspace, key string, index int, val []byte) error
	LRem(workspace, key string, count int, val []byte) (int, error)

	// set
	SAdd(workspace, key string, members ...[]byte) (int, error)
	SRem(workspace, key string, members ...[]byte) (int, error)
	SMembers(workspace, key string) ([][]byte, error)
	SIsMember(workspace, key string, member []byte) (bool, error)
	SCard(workspace, key string) (int, error)
	SInter(workspace string, keys ...string) ([][]byte, error)
	SUnion(workspace string, keys ...string) ([][]byte, error)
	SDiff(workspace string, keys ...string) ([][]byte, error)

	// sorted set
	ZAdd(workspace, key string, members map[string]float64) (int, error)
	ZRem(workspace, key string, members ...string) (int, error)
	ZRange(workspace, key string, start, stop int, withScores bool) ([]ZMember, error)
	ZRevRange(workspace, key string, start, stop int, withScores bool) ([]ZMember, error)
	ZScore(workspace, key, member string) (float64, bool, error)
	ZRank(workspace, key, member string) (int, bool, error)
	ZCard(workspace, key string) (int, error)
	ZIncrBy(workspace, key, member string, delta float64) (float64, error)
	ZRangeByScore(workspace, key string, min, max float64) ([]ZMember, error)

	// transactions (see internal/server/resp/handlers_tx.go)
	// WatchVersion returns the current mutation-version counter for a key,
	// used by WATCH/EXEC optimistic locking.
	WatchVersion(workspace, key string) uint64
	// Exec runs fn while holding the store's global transaction lock,
	// guaranteeing no other transaction interleaves. See store.go for the
	// Phase 1 "single global mutex" tradeoff rationale.
	Exec(fn func() error) error

	// Close stops any background goroutines (e.g. the TTL reaper).
	Close() error
}

// ErrWrongType is returned by data-structure operations when a key exists
// but holds a value of a different type. RESP handlers translate this into
// the Redis-shaped "WRONGTYPE Operation against a key holding the wrong kind
// of value" error string.
var ErrWrongType = wrongTypeError{}

type wrongTypeError struct{}

func (wrongTypeError) Error() string { return "WRONGTYPE" }

// ErrNotInteger is returned when a value expected to be parsed as an integer
// is not (e.g. HINCRBY on a non-numeric field).
var ErrNotInteger = notIntegerError{}

type notIntegerError struct{}

func (notIntegerError) Error() string { return "value is not an integer or out of range" }

// ErrNotFloat is returned when a value expected to be parsed as a float is not.
var ErrNotFloat = notFloatError{}

type notFloatError struct{}

func (notFloatError) Error() string { return "value is not a valid float" }

// ErrIndexOutOfRange is returned by list operations like LSET on an
// out-of-bounds index.
var ErrIndexOutOfRange = indexOutOfRangeError{}

type indexOutOfRangeError struct{}

func (indexOutOfRangeError) Error() string { return "index out of range" }

// ErrNoSuchKey is returned by operations (like LSET) that require the key to
// already exist.
var ErrNoSuchKey = noSuchKeyError{}

type noSuchKeyError struct{}

func (noSuchKeyError) Error() string { return "no such key" }

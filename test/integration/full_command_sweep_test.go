// This file's job is coverage breadth, not depth: drive every RESP command
// in api/commands.yaml that main_test.go/auth_workspace_test.go/
// metrics_test.go don't already exercise end to end, against a real
// compiled server over the real wire, using the default mock providers (no
// network access or API key needed, so this is fully CI-safe and free).
// Exact per-command behavior (error paths, edge cases, exact wire framing)
// is already unit-tested in internal/server/resp's own _test.go files --
// this file exists so "does every command actually work against a real
// running server" has one committed, repeatable answer instead of living
// only in a throwaway manual check.
package integration

import (
	"bufio"
	"context"
	"io"
	"net"
	"testing"

	"github.com/SumitKumar-17/cache-pot/internal/server"
)

// TestGenericCommandsSweep covers every command in RegisterGeneric not
// already exercised elsewhere in this package: DEL, EXISTS, EXPIRE,
// PEXPIRE, TTL, PTTL, PERSIST, TYPE, KEYS, SCAN, RENAME, FLUSHDB, FLUSHALL.
func TestGenericCommandsSweep(t *testing.T) {
	addr := startServer(t)
	rdb := newClient(addr)
	defer rdb.Close()
	ctx := context.Background()

	if err := rdb.Do(ctx, "SET", "gk1", "v1").Err(); err != nil {
		t.Fatalf("SET: %v", err)
	}
	if err := rdb.Do(ctx, "SET", "gk2", "v2").Err(); err != nil {
		t.Fatalf("SET: %v", err)
	}

	if n, err := rdb.Do(ctx, "EXISTS", "gk1", "gk2", "nosuchkey").Int(); err != nil || n != 2 {
		t.Fatalf("EXISTS = (%d, %v), want (2, nil)", n, err)
	}
	if ok, err := rdb.Do(ctx, "EXPIRE", "gk1", "100").Int(); err != nil || ok != 1 {
		t.Fatalf("EXPIRE = (%d, %v), want (1, nil)", ok, err)
	}
	if ok, err := rdb.Do(ctx, "PEXPIRE", "gk2", "100000").Int(); err != nil || ok != 1 {
		t.Fatalf("PEXPIRE = (%d, %v), want (1, nil)", ok, err)
	}
	if ttl, err := rdb.Do(ctx, "TTL", "gk1").Int(); err != nil || ttl <= 0 || ttl > 100 {
		t.Fatalf("TTL = (%d, %v), want a positive value <= 100", ttl, err)
	}
	if pttl, err := rdb.Do(ctx, "PTTL", "gk2").Int(); err != nil || pttl <= 0 || pttl > 100000 {
		t.Fatalf("PTTL = (%d, %v), want a positive value <= 100000", pttl, err)
	}
	if ok, err := rdb.Do(ctx, "PERSIST", "gk1").Int(); err != nil || ok != 1 {
		t.Fatalf("PERSIST = (%d, %v), want (1, nil)", ok, err)
	}
	if ttl, err := rdb.Do(ctx, "TTL", "gk1").Int(); err != nil || ttl != -1 {
		t.Fatalf("TTL after PERSIST = (%d, %v), want (-1, nil)", ttl, err)
	}
	if typ, err := rdb.Do(ctx, "TYPE", "gk1").Text(); err != nil || typ != "string" {
		t.Fatalf("TYPE = (%q, %v), want (\"string\", nil)", typ, err)
	}
	keys, err := rdb.Do(ctx, "KEYS", "gk*").StringSlice()
	if err != nil || len(keys) != 2 {
		t.Fatalf("KEYS gk* = (%v, %v), want 2 matching keys", keys, err)
	}
	if _, err := rdb.Do(ctx, "SCAN", "0").Result(); err != nil {
		t.Fatalf("SCAN: %v", err)
	}
	if err := rdb.Do(ctx, "RENAME", "gk1", "gk1-renamed").Err(); err != nil {
		t.Fatalf("RENAME: %v", err)
	}
	if val, err := rdb.Get(ctx, "gk1-renamed").Result(); err != nil || val != "v1" {
		t.Fatalf("GET gk1-renamed = (%q, %v), want (\"v1\", nil)", val, err)
	}
	if n, err := rdb.Do(ctx, "DEL", "gk1-renamed", "gk2").Int(); err != nil || n != 2 {
		t.Fatalf("DEL = (%d, %v), want (2, nil)", n, err)
	}
	if err := rdb.Do(ctx, "SET", "will-flush", "x").Err(); err != nil {
		t.Fatalf("SET: %v", err)
	}
	if err := rdb.Do(ctx, "FLUSHDB").Err(); err != nil {
		t.Fatalf("FLUSHDB: %v", err)
	}
	if n, err := rdb.Do(ctx, "EXISTS", "will-flush").Int(); err != nil || n != 0 {
		t.Fatalf("EXISTS after FLUSHDB = (%d, %v), want (0, nil)", n, err)
	}
	if err := rdb.Do(ctx, "SET", "will-flushall", "x").Err(); err != nil {
		t.Fatalf("SET: %v", err)
	}
	if err := rdb.Do(ctx, "FLUSHALL").Err(); err != nil {
		t.Fatalf("FLUSHALL: %v", err)
	}
}

// TestConnectionCommandsSweep covers PING, ECHO, SELECT, CLIENT, COMMAND,
// INFO, and single-password AUTH end to end (multi-workspace AUTH already
// has its own dedicated file, auth_workspace_test.go).
func TestConnectionCommandsSweep(t *testing.T) {
	addr := startServerWithConfig(t, serverConfigWithPassword("s3cret"))
	rdb := newClient(addr)
	defer rdb.Close()
	ctx := context.Background()

	// Unauthenticated: only AllowedNoAuth commands work.
	if err := rdb.Do(ctx, "PING").Err(); err == nil {
		t.Fatal("PING before AUTH (password set) succeeded, want NOAUTH")
	}
	if err := rdb.Do(ctx, "AUTH", "wrong-password").Err(); err == nil {
		t.Fatal("AUTH with wrong password succeeded, want WRONGPASS")
	}
	if err := rdb.Do(ctx, "AUTH", "s3cret").Err(); err != nil {
		t.Fatalf("AUTH with correct password: %v", err)
	}

	if pong, err := rdb.Do(ctx, "PING").Text(); err != nil || pong != "PONG" {
		t.Fatalf("PING = (%q, %v), want (\"PONG\", nil)", pong, err)
	}
	if echoed, err := rdb.Do(ctx, "ECHO", "hello").Text(); err != nil || echoed != "hello" {
		t.Fatalf("ECHO = (%q, %v), want (\"hello\", nil)", echoed, err)
	}
	if err := rdb.Do(ctx, "SELECT", "0").Err(); err != nil {
		t.Fatalf("SELECT 0: %v", err)
	}
	if err := rdb.Do(ctx, "SELECT", "1").Err(); err == nil {
		t.Fatal("SELECT 1 succeeded, want an error (only database 0 exists)")
	}
	if err := rdb.Do(ctx, "CLIENT", "SETNAME", "sweep-test").Err(); err != nil {
		t.Fatalf("CLIENT SETNAME: %v", err)
	}
	if name, err := rdb.Do(ctx, "CLIENT", "GETNAME").Text(); err != nil || name != "sweep-test" {
		t.Fatalf("CLIENT GETNAME = (%q, %v), want (\"sweep-test\", nil)", name, err)
	}
	if n, err := rdb.Do(ctx, "COMMAND", "COUNT").Int(); err != nil || n == 0 {
		t.Fatalf("COMMAND COUNT = (%d, %v), want > 0", n, err)
	}
	if info, err := rdb.Do(ctx, "INFO").Text(); err != nil || len(info) == 0 {
		t.Fatalf("INFO = (%q, %v), want a non-empty info string", info, err)
	}
}

// TestQuitClosesConnection covers QUIT specifically: over a raw connection
// (so the test can observe the actual TCP close rather than fighting
// go-redis's own pool/reconnect logic), QUIT must reply +OK and then close
// the connection -- a subsequent read must see EOF.
func TestQuitClosesConnection(t *testing.T) {
	addr := startServer(t)
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	sendInline(t, conn, "QUIT")
	r := bufio.NewReader(conn)
	if line := mustReadLine(t, r); line != "+OK" {
		t.Fatalf("QUIT reply = %q, want +OK", line)
	}
	if _, err := r.ReadByte(); err != io.EOF {
		t.Fatalf("read after QUIT = %v, want io.EOF (server should close the connection)", err)
	}
}

// TestPubSubCommandsSweep covers SUBSCRIBE, UNSUBSCRIBE, PUBLISH,
// PSUBSCRIBE, and PUNSUBSCRIBE end to end over two real connections (a
// publisher and a subscriber) -- this package had zero pub/sub coverage
// before this file.
func TestPubSubCommandsSweep(t *testing.T) {
	addr := startServer(t)
	pub := newClient(addr)
	defer pub.Close()
	sub := newClient(addr)
	defer sub.Close()
	ctx := context.Background()

	direct := sub.Subscribe(ctx, "news")
	defer direct.Close()
	if _, err := direct.Receive(ctx); err != nil {
		t.Fatalf("SUBSCRIBE confirmation: %v", err)
	}

	pattern := sub.PSubscribe(ctx, "news.*")
	defer pattern.Close()
	if _, err := pattern.Receive(ctx); err != nil {
		t.Fatalf("PSUBSCRIBE confirmation: %v", err)
	}

	if n, err := pub.Publish(ctx, "news", "hello-direct").Result(); err != nil || n != 1 {
		t.Fatalf("PUBLISH news = (%d, %v), want (1, nil) -- only the direct subscriber matches", n, err)
	}
	msg, err := direct.ReceiveMessage(ctx)
	if err != nil {
		t.Fatalf("receive on direct subscription: %v", err)
	}
	if msg.Payload != "hello-direct" || msg.Channel != "news" {
		t.Fatalf("direct message = %+v, want payload=hello-direct channel=news", msg)
	}

	if n, err := pub.Publish(ctx, "news.5", "hello-pattern").Result(); err != nil || n != 1 {
		t.Fatalf("PUBLISH news.5 = (%d, %v), want (1, nil) -- only the pattern subscriber matches", n, err)
	}
	pmsg, err := pattern.ReceiveMessage(ctx)
	if err != nil {
		t.Fatalf("receive on pattern subscription: %v", err)
	}
	if pmsg.Payload != "hello-pattern" || pmsg.Channel != "news.5" || pmsg.Pattern != "news.*" {
		t.Fatalf("pattern message = %+v, want payload=hello-pattern channel=news.5 pattern=news.*", pmsg)
	}

	if err := direct.Unsubscribe(ctx, "news"); err != nil {
		t.Fatalf("UNSUBSCRIBE: %v", err)
	}
	if err := pattern.PUnsubscribe(ctx, "news.*"); err != nil {
		t.Fatalf("PUNSUBSCRIBE: %v", err)
	}
	if n, err := pub.Publish(ctx, "news", "after-unsubscribe").Result(); err != nil || n != 0 {
		t.Fatalf("PUBLISH after UNSUBSCRIBE = (%d, %v), want (0, nil)", n, err)
	}
}

// TestCachePromptAndToolCacheSweep covers CACHE.PROMPT and TOOL.CACHE end
// to end -- neither is an embedding-based cache (both are exact-match), so
// neither needed the real-OpenAI provider file; this package had zero
// coverage of either before this file.
func TestCachePromptAndToolCacheSweep(t *testing.T) {
	addr := startServer(t)
	rdb := newClient(addr)
	defer rdb.Close()
	ctx := context.Background()

	if err := rdb.Do(ctx, "CACHE.PROMPT", "SET", "Summarize: {{.x}}", `{"x":1}`, "gpt-test", "a summary").Err(); err != nil {
		t.Fatalf("CACHE.PROMPT SET: %v", err)
	}
	// Key order in the variables JSON must not matter.
	if val, err := rdb.Do(ctx, "CACHE.PROMPT", "GET", "Summarize: {{.x}}", `{"x":1}`, "gpt-test").Text(); err != nil || val != "a summary" {
		t.Fatalf("CACHE.PROMPT GET = (%q, %v), want (\"a summary\", nil)", val, err)
	}
	if err := rdb.Do(ctx, "CACHE.PROMPT", "GET", "Summarize: {{.x}}", `{"x":2}`, "gpt-test").Err(); err == nil {
		t.Fatal("CACHE.PROMPT GET with different variables succeeded, want a miss (redis.Nil)")
	}

	if err := rdb.Do(ctx, "TOOL.CACHE", "SET", "github.getIssue", `{"repo":"cache-pot","issue":42}`, `{"title":"..."}`).Err(); err != nil {
		t.Fatalf("TOOL.CACHE SET: %v", err)
	}
	// Key order in the args JSON must not matter (canonicalized).
	if val, err := rdb.Do(ctx, "TOOL.CACHE", "GET", "github.getIssue", `{"issue":42,"repo":"cache-pot"}`).Text(); err != nil || val != `{"title":"..."}` {
		t.Fatalf("TOOL.CACHE GET = (%q, %v), want the cached result", val, err)
	}
}

// TestVectorCommandsFullSweep covers VECTOR.UPSERT/SEARCH/DELETE as a full
// round trip (auth_workspace_test.go only touches these to check the
// workspace-isolation boundary, not the actual search/delete behavior).
func TestVectorCommandsFullSweep(t *testing.T) {
	addr := startServer(t)
	rdb := newClient(addr)
	defer rdb.Close()
	ctx := context.Background()

	if err := rdb.Do(ctx, "VECTOR.UPSERT", "docs", "a", "[1,0]", "METADATA", `{"lang":"en"}`, "TEXT", "cats are cute").Err(); err != nil {
		t.Fatalf("VECTOR.UPSERT a: %v", err)
	}
	if err := rdb.Do(ctx, "VECTOR.UPSERT", "docs", "b", "[0,1]").Err(); err != nil {
		t.Fatalf("VECTOR.UPSERT b: %v", err)
	}

	results, err := rdb.Do(ctx, "VECTOR.SEARCH", "docs", "[1,0]", "K", "1", "WITHSCORES").StringSlice()
	if err != nil {
		t.Fatalf("VECTOR.SEARCH: %v", err)
	}
	if len(results) != 2 || results[0] != "a" {
		t.Fatalf("VECTOR.SEARCH = %v, want [\"a\", <score>]", results)
	}

	filtered, err := rdb.Do(ctx, "VECTOR.SEARCH", "docs", "[1,0]", "FILTER", "lang", "en").StringSlice()
	if err != nil {
		t.Fatalf("VECTOR.SEARCH with FILTER: %v", err)
	}
	if len(filtered) != 1 || filtered[0] != "a" {
		t.Fatalf("VECTOR.SEARCH FILTER lang=en = %v, want [\"a\"] only", filtered)
	}

	if n, err := rdb.Do(ctx, "VECTOR.DELETE", "docs", "a").Int(); err != nil || n != 1 {
		t.Fatalf("VECTOR.DELETE a = (%d, %v), want (1, nil)", n, err)
	}
	if n, err := rdb.Do(ctx, "VECTOR.DELETE", "docs", "a").Int(); err != nil || n != 0 {
		t.Fatalf("VECTOR.DELETE a (already gone) = (%d, %v), want (0, nil)", n, err)
	}
}

// TestMemoryVersioningFullSweep covers MEMORY.PUT/GET/SEARCH and
// MEMORY.HISTORY as a full round trip with the mock embedding provider
// (real-embedding semantic ranking has its own dedicated coverage in
// real_openai_test.go).
func TestMemoryVersioningFullSweep(t *testing.T) {
	addr := startServer(t)
	rdb := newClient(addr)
	defer rdb.Close()
	ctx := context.Background()

	if err := rdb.Do(ctx, "MEMORY.PUT", "bot", "note v1", "ID", "mem-1").Err(); err != nil {
		t.Fatalf("MEMORY.PUT v1: %v", err)
	}
	if err := rdb.Do(ctx, "MEMORY.PUT", "bot", "note v2", "ID", "mem-1").Err(); err != nil {
		t.Fatalf("MEMORY.PUT v2: %v", err)
	}

	fields, err := rdb.Do(ctx, "MEMORY.GET", "default", "mem-1").StringSlice()
	if err != nil {
		t.Fatalf("MEMORY.GET: %v", err)
	}
	var content, version string
	for i := 0; i+1 < len(fields); i += 2 {
		switch fields[i] {
		case "content":
			content = fields[i+1]
		case "version":
			version = fields[i+1]
		}
	}
	if content != "note v2" || version != "2" {
		t.Fatalf("MEMORY.GET current version = (content=%q, version=%q), want (\"note v2\", \"2\")", content, version)
	}

	history, err := rdb.Do(ctx, "MEMORY.HISTORY", "default", "mem-1").Result()
	if err != nil {
		t.Fatalf("MEMORY.HISTORY: %v", err)
	}
	versions, ok := history.([]any)
	if !ok || len(versions) != 2 {
		t.Fatalf("MEMORY.HISTORY = %v, want 2 versions", history)
	}

	if _, err := rdb.Do(ctx, "MEMORY.PUT", "bot", "unrelated content about a completely different subject", "KIND", "long_term").Result(); err != nil {
		t.Fatalf("MEMORY.PUT unrelated: %v", err)
	}
	searchResults, err := rdb.Do(ctx, "MEMORY.SEARCH", "default", "note").StringSlice()
	if err != nil {
		t.Fatalf("MEMORY.SEARCH: %v", err)
	}
	if len(searchResults) == 0 {
		t.Fatal("MEMORY.SEARCH found 0 results, want at least mem-1")
	}
}

// TestDataStructureCommandsSweep covers every string/hash/list/set/
// sorted-set command main_test.go doesn't already exercise via go-redis's
// typed methods (SET/GET/HSET/HGETALL/RPUSH/LRANGE/SADD/SMEMBERS/ZADD/
// ZRANGE) -- e.g. MGET/MSET, HDEL/HEXISTS/HKEYS/HVALS/HLEN/HMGET/HINCRBY,
// LPOP/RPOP/LLEN/LINDEX/LSET/LREM, SREM/SISMEMBER/SCARD/SINTER/SUNION/
// SDIFF, ZREM/ZREVRANGE/ZSCORE/ZRANK/ZCARD/ZINCRBY/ZRANGEBYSCORE.
func TestDataStructureCommandsSweep(t *testing.T) {
	addr := startServer(t)
	rdb := newClient(addr)
	defer rdb.Close()
	ctx := context.Background()

	// strings
	if err := rdb.Do(ctx, "MSET", "s1", "a", "s2", "b").Err(); err != nil {
		t.Fatalf("MSET: %v", err)
	}
	// MGET's reply mixes real strings with a nil for the missing key, so
	// this uses the generic Result()/[]any decode rather than StringSlice()
	// (which errors on a nil element -- go-redis's own typed MGet() has the
	// same []any signature for the same reason).
	got, err := rdb.Do(ctx, "MGET", "s1", "s2", "nosuchkey").Result()
	if err != nil {
		t.Fatalf("MGET: %v", err)
	}
	gotSlice, ok := got.([]any)
	if !ok || len(gotSlice) != 3 || gotSlice[0] != "a" || gotSlice[1] != "b" || gotSlice[2] != nil {
		t.Fatalf("MGET = %v, want [a b <nil>]", got)
	}
	if err := rdb.Do(ctx, "SET", "cnt", "10").Err(); err != nil {
		t.Fatalf("SET cnt: %v", err)
	}
	if n, err := rdb.Do(ctx, "INCRBY", "cnt", "5").Int(); err != nil || n != 15 {
		t.Fatalf("INCRBY = (%d, %v), want (15, nil)", n, err)
	}
	if n, err := rdb.Do(ctx, "DECR", "cnt").Int(); err != nil || n != 14 {
		t.Fatalf("DECR = (%d, %v), want (14, nil)", n, err)
	}
	if n, err := rdb.Do(ctx, "DECRBY", "cnt", "4").Int(); err != nil || n != 10 {
		t.Fatalf("DECRBY = (%d, %v), want (10, nil)", n, err)
	}
	if err := rdb.Do(ctx, "SET", "str", "hello").Err(); err != nil {
		t.Fatalf("SET str: %v", err)
	}
	if n, err := rdb.Do(ctx, "APPEND", "str", " world").Int(); err != nil || n != 11 {
		t.Fatalf("APPEND = (%d, %v), want (11, nil)", n, err)
	}
	if n, err := rdb.Do(ctx, "STRLEN", "str").Int(); err != nil || n != 11 {
		t.Fatalf("STRLEN = (%d, %v), want (11, nil)", n, err)
	}

	// hashes
	if err := rdb.Do(ctx, "HSET", "h1", "f1", "v1", "f2", "v2").Err(); err != nil {
		t.Fatalf("HSET: %v", err)
	}
	if val, err := rdb.Do(ctx, "HGET", "h1", "f1").Text(); err != nil || val != "v1" {
		t.Fatalf("HGET = (%q, %v), want (\"v1\", nil)", val, err)
	}
	if n, err := rdb.Do(ctx, "HEXISTS", "h1", "f1").Int(); err != nil || n != 1 {
		t.Fatalf("HEXISTS = (%d, %v), want (1, nil)", n, err)
	}
	if keys, err := rdb.Do(ctx, "HKEYS", "h1").StringSlice(); err != nil || len(keys) != 2 {
		t.Fatalf("HKEYS = (%v, %v), want 2 fields", keys, err)
	}
	if vals, err := rdb.Do(ctx, "HVALS", "h1").StringSlice(); err != nil || len(vals) != 2 {
		t.Fatalf("HVALS = (%v, %v), want 2 values", vals, err)
	}
	if n, err := rdb.Do(ctx, "HLEN", "h1").Int(); err != nil || n != 2 {
		t.Fatalf("HLEN = (%d, %v), want (2, nil)", n, err)
	}
	if vals, err := rdb.Do(ctx, "HMGET", "h1", "f1", "f2").StringSlice(); err != nil || len(vals) != 2 {
		t.Fatalf("HMGET = (%v, %v), want 2 values", vals, err)
	}
	if err := rdb.Do(ctx, "HSET", "h1", "n", "1").Err(); err != nil {
		t.Fatalf("HSET n: %v", err)
	}
	if n, err := rdb.Do(ctx, "HINCRBY", "h1", "n", "4").Int(); err != nil || n != 5 {
		t.Fatalf("HINCRBY = (%d, %v), want (5, nil)", n, err)
	}
	if n, err := rdb.Do(ctx, "HDEL", "h1", "f2").Int(); err != nil || n != 1 {
		t.Fatalf("HDEL = (%d, %v), want (1, nil)", n, err)
	}

	// lists
	if err := rdb.Do(ctx, "RPUSH", "l1", "a", "b", "c").Err(); err != nil {
		t.Fatalf("RPUSH: %v", err)
	}
	if err := rdb.Do(ctx, "LPUSH", "l1", "z").Err(); err != nil {
		t.Fatalf("LPUSH: %v", err)
	}
	if n, err := rdb.Do(ctx, "LLEN", "l1").Int(); err != nil || n != 4 {
		t.Fatalf("LLEN = (%d, %v), want (4, nil)", n, err)
	}
	if val, err := rdb.Do(ctx, "LINDEX", "l1", "0").Text(); err != nil || val != "z" {
		t.Fatalf("LINDEX = (%q, %v), want (\"z\", nil)", val, err)
	}
	if err := rdb.Do(ctx, "LSET", "l1", "0", "z2").Err(); err != nil {
		t.Fatalf("LSET: %v", err)
	}
	if n, err := rdb.Do(ctx, "LREM", "l1", "1", "z2").Int(); err != nil || n != 1 {
		t.Fatalf("LREM = (%d, %v), want (1, nil)", n, err)
	}
	if val, err := rdb.Do(ctx, "LPOP", "l1").Text(); err != nil || val != "a" {
		t.Fatalf("LPOP = (%q, %v), want (\"a\", nil)", val, err)
	}
	if val, err := rdb.Do(ctx, "RPOP", "l1").Text(); err != nil || val != "c" {
		t.Fatalf("RPOP = (%q, %v), want (\"c\", nil)", val, err)
	}

	// sets
	if err := rdb.Do(ctx, "SADD", "set1", "x", "y").Err(); err != nil {
		t.Fatalf("SADD set1: %v", err)
	}
	if err := rdb.Do(ctx, "SADD", "set2", "y", "z").Err(); err != nil {
		t.Fatalf("SADD set2: %v", err)
	}
	if n, err := rdb.Do(ctx, "SISMEMBER", "set1", "x").Int(); err != nil || n != 1 {
		t.Fatalf("SISMEMBER = (%d, %v), want (1, nil)", n, err)
	}
	if n, err := rdb.Do(ctx, "SCARD", "set1").Int(); err != nil || n != 2 {
		t.Fatalf("SCARD = (%d, %v), want (2, nil)", n, err)
	}
	if members, err := rdb.Do(ctx, "SINTER", "set1", "set2").StringSlice(); err != nil || len(members) != 1 {
		t.Fatalf("SINTER = (%v, %v), want 1 member (y)", members, err)
	}
	if members, err := rdb.Do(ctx, "SUNION", "set1", "set2").StringSlice(); err != nil || len(members) != 3 {
		t.Fatalf("SUNION = (%v, %v), want 3 members", members, err)
	}
	if members, err := rdb.Do(ctx, "SDIFF", "set1", "set2").StringSlice(); err != nil || len(members) != 1 {
		t.Fatalf("SDIFF = (%v, %v), want 1 member (x)", members, err)
	}
	if n, err := rdb.Do(ctx, "SREM", "set1", "x").Int(); err != nil || n != 1 {
		t.Fatalf("SREM = (%d, %v), want (1, nil)", n, err)
	}

	// sorted sets
	if err := rdb.Do(ctx, "ZADD", "z1", "1", "a", "2", "b", "3", "c").Err(); err != nil {
		t.Fatalf("ZADD: %v", err)
	}
	if members, err := rdb.Do(ctx, "ZREVRANGE", "z1", "0", "-1").StringSlice(); err != nil || len(members) != 3 || members[0] != "c" {
		t.Fatalf("ZREVRANGE = (%v, %v), want [c b a]", members, err)
	}
	if score, err := rdb.Do(ctx, "ZSCORE", "z1", "b").Text(); err != nil || score != "2" {
		t.Fatalf("ZSCORE = (%q, %v), want (\"2\", nil)", score, err)
	}
	if rank, err := rdb.Do(ctx, "ZRANK", "z1", "b").Int(); err != nil || rank != 1 {
		t.Fatalf("ZRANK = (%d, %v), want (1, nil)", rank, err)
	}
	if n, err := rdb.Do(ctx, "ZCARD", "z1").Int(); err != nil || n != 3 {
		t.Fatalf("ZCARD = (%d, %v), want (3, nil)", n, err)
	}
	if score, err := rdb.Do(ctx, "ZINCRBY", "z1", "5", "a").Text(); err != nil || score != "6" {
		t.Fatalf("ZINCRBY = (%q, %v), want (\"6\", nil)", score, err)
	}
	if members, err := rdb.Do(ctx, "ZRANGEBYSCORE", "z1", "2", "3").StringSlice(); err != nil || len(members) != 2 {
		t.Fatalf("ZRANGEBYSCORE = (%v, %v), want 2 members (b, c)", members, err)
	}
	if n, err := rdb.Do(ctx, "ZREM", "z1", "c").Int(); err != nil || n != 1 {
		t.Fatalf("ZREM = (%d, %v), want (1, nil)", n, err)
	}
}

// TestTransactionDiscardAndUnwatchSweep covers DISCARD and UNWATCH
// specifically -- main_test.go's TestMultiExec/TestWatchAbort already cover
// MULTI/EXEC/WATCH's success and abort paths, but neither ever calls
// DISCARD or UNWATCH.
func TestTransactionDiscardAndUnwatchSweep(t *testing.T) {
	addr := startServer(t)
	rdb := newClient(addr)
	defer rdb.Close()
	ctx := context.Background()

	if err := rdb.Do(ctx, "WATCH", "some-key").Err(); err != nil {
		t.Fatalf("WATCH: %v", err)
	}
	if err := rdb.Do(ctx, "UNWATCH").Err(); err != nil {
		t.Fatalf("UNWATCH: %v", err)
	}

	if err := rdb.Do(ctx, "MULTI").Err(); err != nil {
		t.Fatalf("MULTI: %v", err)
	}
	if err := rdb.Do(ctx, "SET", "discarded-key", "v").Err(); err != nil {
		t.Fatalf("SET (queued): %v", err)
	}
	if err := rdb.Do(ctx, "DISCARD").Err(); err != nil {
		t.Fatalf("DISCARD: %v", err)
	}
	if n, err := rdb.Do(ctx, "EXISTS", "discarded-key").Int(); err != nil || n != 0 {
		t.Fatalf("EXISTS discarded-key after DISCARD = (%d, %v), want (0, nil) -- DISCARD must drop the queued SET", n, err)
	}
}

// serverConfigWithPassword builds the minimal server.Config for a
// single-shared-password test server, reusing the same MaxConnections
// convention every other helper in this package uses.
func serverConfigWithPassword(password string) server.Config {
	return server.Config{MaxConnections: 1000, Password: password}
}

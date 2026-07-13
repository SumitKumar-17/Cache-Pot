// Package integration drives a real, in-process Cache-Pot server over the
// wire (both via the go-redis client and, for one raw-protocol check, a
// plain net.Dial connection) to validate Phase 1 end to end.
package integration

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/SumitKumar-17/cache-pot/internal/server"
)

// startServer boots a real server.Run instance on a random free port
// (picked via net.Listen(":0"), handed straight to server.RunListener so
// there's no bind-race) and returns its address plus a cleanup func.
func startServer(t *testing.T) string {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := server.RunListener(ctx, server.Config{MaxConnections: 1000}, ln); err != nil {
			t.Errorf("server.RunListener: %v", err)
		}
	}()

	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("server did not shut down in time")
		}
	})

	return addr
}

// newClient builds a go-redis client explicitly pinned to RESP2, since
// go-redis v9 defaults to requesting RESP3 (Protocol 3) via HELLO when
// Protocol is left unset, and Phase 1 only supports RESP2.
func newClient(addr string) *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr:     addr,
		Protocol: 2,
	})
}

func TestStringOps(t *testing.T) {
	addr := startServer(t)
	rdb := newClient(addr)
	defer rdb.Close()
	ctx := context.Background()

	if err := rdb.Set(ctx, "foo", "bar", 0).Err(); err != nil {
		t.Fatalf("SET: %v", err)
	}
	val, err := rdb.Get(ctx, "foo").Result()
	if err != nil || val != "bar" {
		t.Fatalf("GET: val=%q err=%v", val, err)
	}

	// Use PEXPIRE (millisecond granularity) rather than go-redis's Expire,
	// which truncates any sub-second duration up to a full second.
	if err := rdb.PExpire(ctx, "foo", 100*time.Millisecond).Err(); err != nil {
		t.Fatalf("PEXPIRE: %v", err)
	}
	ttl, err := rdb.PTTL(ctx, "foo").Result()
	if err != nil || ttl <= 0 {
		t.Fatalf("PTTL: ttl=%v err=%v", ttl, err)
	}

	time.Sleep(200 * time.Millisecond)
	_, err = rdb.Get(ctx, "foo").Result()
	if err != redis.Nil {
		t.Fatalf("expected key expired (redis.Nil), got err=%v", err)
	}
}

func TestHashOps(t *testing.T) {
	addr := startServer(t)
	rdb := newClient(addr)
	defer rdb.Close()
	ctx := context.Background()

	if err := rdb.HSet(ctx, "h", "f1", "v1", "f2", "v2").Err(); err != nil {
		t.Fatalf("HSET: %v", err)
	}
	got, err := rdb.HGetAll(ctx, "h").Result()
	if err != nil {
		t.Fatalf("HGETALL: %v", err)
	}
	want := map[string]string{"f1": "v1", "f2": "v2"}
	if len(got) != len(want) || got["f1"] != "v1" || got["f2"] != "v2" {
		t.Fatalf("HGETALL = %v, want %v", got, want)
	}
}

func TestListOps(t *testing.T) {
	addr := startServer(t)
	rdb := newClient(addr)
	defer rdb.Close()
	ctx := context.Background()

	if err := rdb.RPush(ctx, "l", "a", "b", "c").Err(); err != nil {
		t.Fatalf("RPUSH: %v", err)
	}
	got, err := rdb.LRange(ctx, "l", 0, -1).Result()
	if err != nil {
		t.Fatalf("LRANGE: %v", err)
	}
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("LRANGE = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("LRANGE[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestSetOps(t *testing.T) {
	addr := startServer(t)
	rdb := newClient(addr)
	defer rdb.Close()
	ctx := context.Background()

	if err := rdb.SAdd(ctx, "s", "x", "y", "z").Err(); err != nil {
		t.Fatalf("SADD: %v", err)
	}
	got, err := rdb.SMembers(ctx, "s").Result()
	if err != nil || len(got) != 3 {
		t.Fatalf("SMEMBERS = %v, err=%v", got, err)
	}
}

func TestZSetOps(t *testing.T) {
	addr := startServer(t)
	rdb := newClient(addr)
	defer rdb.Close()
	ctx := context.Background()

	if err := rdb.ZAdd(ctx, "z",
		redis.Z{Score: 1, Member: "a"},
		redis.Z{Score: 2, Member: "b"},
		redis.Z{Score: 3, Member: "c"},
	).Err(); err != nil {
		t.Fatalf("ZADD: %v", err)
	}
	got, err := rdb.ZRange(ctx, "z", 0, -1).Result()
	if err != nil {
		t.Fatalf("ZRANGE: %v", err)
	}
	want := []string{"a", "b", "c"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ZRANGE[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestMultiExec(t *testing.T) {
	addr := startServer(t)
	rdb := newClient(addr)
	defer rdb.Close()
	ctx := context.Background()

	pipe := rdb.TxPipeline()
	incr := pipe.Incr(ctx, "counter")
	pipe.Incr(ctx, "counter")
	_, err := pipe.Exec(ctx)
	if err != nil {
		t.Fatalf("MULTI/EXEC: %v", err)
	}
	if incr.Val() != 1 {
		t.Fatalf("first INCR in transaction = %d, want 1", incr.Val())
	}
	val, err := rdb.Get(ctx, "counter").Result()
	if err != nil || val != "2" {
		t.Fatalf("counter after transaction = %q, err=%v", val, err)
	}
}

// TestWatchAbort drives WATCH/MULTI/EXEC at the raw connection level (two
// separate net.Conn's to the same server) since go-redis's high-level
// client doesn't expose a WATCH primitive that lets us force the specific
// interleaving needed to trigger an abort.
func TestWatchAbort(t *testing.T) {
	addr := startServer(t)

	c1, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial c1: %v", err)
	}
	defer c1.Close()
	c2, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial c2: %v", err)
	}
	defer c2.Close()

	r1 := bufio.NewReader(c1)
	r2 := bufio.NewReader(c2)

	sendInline(t, c1, "SET k v0")
	mustReadLine(t, r1) // +OK

	sendInline(t, c1, "WATCH k")
	mustReadLine(t, r1) // +OK

	sendInline(t, c1, "MULTI")
	mustReadLine(t, r1) // +OK

	sendInline(t, c1, "SET k v1")
	mustReadLine(t, r1) // +QUEUED

	// c2 mutates the watched key from another connection in between.
	sendInline(t, c2, "SET k from-c2")
	mustReadLine(t, r2) // +OK

	sendInline(t, c1, "EXEC")
	line := mustReadLine(t, r1)
	if line != "*-1" {
		t.Fatalf("expected EXEC to abort with a null array (*-1), got %q", line)
	}

	// The key should reflect c2's write, not the aborted transaction's.
	sendInline(t, c1, "GET k")
	mustReadLine(t, r1) // bulk length line, e.g. $7
	val := mustReadLine(t, r1)
	if val != "from-c2" {
		t.Fatalf("GET k = %q, want %q", val, "from-c2")
	}
}

// TestPipelining sends N commands back-to-back without waiting for
// individual replies, then verifies N in-order replies come back.
func TestPipelining(t *testing.T) {
	addr := startServer(t)
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	const n = 20
	var sb strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&sb, "SET k%d v%d\r\n", i, i)
	}
	if _, err := conn.Write([]byte(sb.String())); err != nil {
		t.Fatalf("write pipeline: %v", err)
	}

	r := bufio.NewReader(conn)
	for i := 0; i < n; i++ {
		line := mustReadLine(t, r)
		if line != "+OK" {
			t.Fatalf("pipelined reply %d = %q, want +OK", i, line)
		}
	}
}

// TestHello3RawProtocolError verifies, at the raw wire level (bypassing any
// client library), that sending HELLO 3 gets back a clean RESP2 error reply
// instead of a hang or a broken connection — real client libraries probe
// this on connect, so a bad rejection path would break compatibility even
// though RESP3 itself is out of Phase 1 scope.
func TestHello3RawProtocolError(t *testing.T) {
	addr := startServer(t)
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	sendInline(t, conn, "HELLO 3")

	r := bufio.NewReader(conn)
	line := mustReadLine(t, r)
	if len(line) == 0 || line[0] != '-' {
		t.Fatalf("expected a RESP error reply to HELLO 3, got %q", line)
	}

	// The connection must still be usable afterwards (not left broken).
	sendInline(t, conn, "PING")
	pong := mustReadLine(t, r)
	if pong != "+PONG" {
		t.Fatalf("PING after HELLO 3 error = %q, want +PONG", pong)
	}
}

func sendInline(t *testing.T, conn net.Conn, line string) {
	t.Helper()
	if _, err := conn.Write([]byte(line + "\r\n")); err != nil {
		t.Fatalf("write %q: %v", line, err)
	}
}

func mustReadLine(t *testing.T, r *bufio.Reader) string {
	t.Helper()
	line, err := r.ReadString('\n')
	if err != nil {
		t.Fatalf("read line: %v", err)
	}
	for len(line) > 0 && (line[len(line)-1] == '\n' || line[len(line)-1] == '\r') {
		line = line[:len(line)-1]
	}
	return line
}

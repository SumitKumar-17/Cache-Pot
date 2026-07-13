package resp

import (
	"bufio"
	"net"
	"testing"
	"time"
)

// newPipeConn drives a real HandleConn goroutine over an in-memory net.Pipe,
// so pub/sub delivery (inherently cross-connection) can be exercised the
// same way test/integration's raw-socket tests do, but without a real TCP
// listener. Both ends of the pipe get a generous fixed deadline up front
// (net.Pipe has no latency of its own, so this is just a safety net against
// a genuinely hung test).
func newPipeConn(t *testing.T, deps *Deps) (net.Conn, *bufio.Reader) {
	t.Helper()
	client, server := net.Pipe()
	t.Cleanup(func() { _ = client.Close() })
	_ = client.SetDeadline(time.Now().Add(5 * time.Second))
	go HandleConn(server, deps)
	return client, bufio.NewReader(client)
}

func sendPipeLine(t *testing.T, conn net.Conn, line string) {
	t.Helper()
	if _, err := conn.Write([]byte(line + "\r\n")); err != nil {
		t.Fatalf("write %q: %v", line, err)
	}
}

func TestSubscribePublishDeliversToSubscriber(t *testing.T) {
	deps := newTestDeps(t)
	sub, subR := newPipeConn(t, deps)
	pub, pubR := newPipeConn(t, deps)

	sendPipeLine(t, sub, "SUBSCRIBE news")
	subReply := readRESPValue(t, subR)
	if subReply.kind != '*' || len(subReply.items) != 3 {
		t.Fatalf("SUBSCRIBE reply = %+v, want a 3-element array", subReply)
	}
	if subReply.items[0].str != "subscribe" || subReply.items[1].str != "news" || subReply.items[2].str != "1" {
		t.Fatalf("SUBSCRIBE reply = %+v, want [subscribe news 1]", subReply)
	}

	sendPipeLine(t, pub, "PUBLISH news hello")
	pubReply := readRESPValue(t, pubR)
	if pubReply.kind != ':' || pubReply.str != "1" {
		t.Fatalf("PUBLISH reply = %+v, want an integer 1 (one subscriber)", pubReply)
	}

	push := readRESPValue(t, subR)
	if push.kind != '*' || len(push.items) != 3 {
		t.Fatalf("pushed message = %+v, want a 3-element array", push)
	}
	if push.items[0].str != "message" || push.items[1].str != "news" || push.items[2].str != "hello" {
		t.Fatalf("pushed message = %+v, want [message news hello]", push)
	}
}

func TestPublishNoSubscribersReturnsZeroNotError(t *testing.T) {
	deps := newTestDeps(t)
	pub, pubR := newPipeConn(t, deps)

	sendPipeLine(t, pub, "PUBLISH nobody hello")
	reply := readRESPValue(t, pubR)
	if reply.kind != ':' || reply.str != "0" {
		t.Fatalf("PUBLISH with no subscribers = %+v, want an integer 0", reply)
	}
}

func TestPSubscribePatternMatch(t *testing.T) {
	deps := newTestDeps(t)
	sub, subR := newPipeConn(t, deps)
	pub, pubR := newPipeConn(t, deps)

	sendPipeLine(t, sub, "PSUBSCRIBE news.*")
	subReply := readRESPValue(t, subR)
	if subReply.items[0].str != "psubscribe" || subReply.items[1].str != "news.*" || subReply.items[2].str != "1" {
		t.Fatalf("PSUBSCRIBE reply = %+v, want [psubscribe news.* 1]", subReply)
	}

	sendPipeLine(t, pub, "PUBLISH news.sports score")
	pubReply := readRESPValue(t, pubR)
	if pubReply.str != "1" {
		t.Fatalf("PUBLISH to a pattern-matched channel = %+v, want an integer 1", pubReply)
	}

	push := readRESPValue(t, subR)
	if len(push.items) != 4 {
		t.Fatalf("pmessage push = %+v, want a 4-element array", push)
	}
	if push.items[0].str != "pmessage" || push.items[1].str != "news.*" ||
		push.items[2].str != "news.sports" || push.items[3].str != "score" {
		t.Fatalf("pmessage push = %+v, want [pmessage news.* news.sports score]", push)
	}

	// A channel that doesn't match the pattern gets no delivery and no
	// subscriber count.
	sendPipeLine(t, pub, "PUBLISH other.thing x")
	pubReply = readRESPValue(t, pubR)
	if pubReply.str != "0" {
		t.Fatalf("PUBLISH to a non-matching channel = %+v, want an integer 0", pubReply)
	}
}

func TestUnsubscribeStopsDelivery(t *testing.T) {
	deps := newTestDeps(t)
	sub, subR := newPipeConn(t, deps)
	pub, pubR := newPipeConn(t, deps)

	sendPipeLine(t, sub, "SUBSCRIBE news")
	readRESPValue(t, subR) // subscribe ack

	sendPipeLine(t, sub, "UNSUBSCRIBE news")
	unsubReply := readRESPValue(t, subR)
	if unsubReply.items[0].str != "unsubscribe" || unsubReply.items[1].str != "news" || unsubReply.items[2].str != "0" {
		t.Fatalf("UNSUBSCRIBE reply = %+v, want [unsubscribe news 0]", unsubReply)
	}

	sendPipeLine(t, pub, "PUBLISH news hello")
	pubReply := readRESPValue(t, pubR)
	if pubReply.str != "0" {
		t.Fatalf("PUBLISH after UNSUBSCRIBE = %+v, want an integer 0 (no subscribers left)", pubReply)
	}
}

func TestPUnsubscribeStopsDelivery(t *testing.T) {
	deps := newTestDeps(t)
	sub, subR := newPipeConn(t, deps)
	pub, pubR := newPipeConn(t, deps)

	sendPipeLine(t, sub, "PSUBSCRIBE news.*")
	readRESPValue(t, subR) // psubscribe ack

	sendPipeLine(t, sub, "PUNSUBSCRIBE news.*")
	unsubReply := readRESPValue(t, subR)
	if unsubReply.items[0].str != "punsubscribe" || unsubReply.items[1].str != "news.*" || unsubReply.items[2].str != "0" {
		t.Fatalf("PUNSUBSCRIBE reply = %+v, want [punsubscribe news.* 0]", unsubReply)
	}

	sendPipeLine(t, pub, "PUBLISH news.sports hello")
	pubReply := readRESPValue(t, pubR)
	if pubReply.str != "0" {
		t.Fatalf("PUBLISH after PUNSUBSCRIBE = %+v, want an integer 0 (no subscribers left)", pubReply)
	}
}

// TestPUnsubscribeNoArgsUnsubscribesAllPatterns mirrors UNSUBSCRIBE's
// no-args-means-everything behavior for patterns.
func TestPUnsubscribeNoArgsUnsubscribesAllPatterns(t *testing.T) {
	deps := newTestDeps(t)
	sub, subR := newPipeConn(t, deps)

	sendPipeLine(t, sub, "PSUBSCRIBE news.* sports.*")
	readRESPValue(t, subR)
	readRESPValue(t, subR)

	sendPipeLine(t, sub, "PUNSUBSCRIBE")
	first := readRESPValue(t, subR)
	second := readRESPValue(t, subR)
	if first.items[0].str != "punsubscribe" || second.items[0].str != "punsubscribe" {
		t.Fatalf("PUNSUBSCRIBE (no args) replies = %+v, %+v, want two punsubscribe acks", first, second)
	}
	if first.items[2].str != "1" || second.items[2].str != "0" {
		t.Fatalf("PUNSUBSCRIBE (no args) counts = %s, %s, want descending to 0", first.items[2].str, second.items[2].str)
	}
}

func TestPubSubCommandsWrongArity(t *testing.T) {
	cs := newTestClientState(t)

	out := execCommand(t, cs, "SUBSCRIBE")
	want := "-" + ErrWrongNumberOfArgs("subscribe") + "\r\n"
	if string(out) != want {
		t.Fatalf("SUBSCRIBE (no channel) = %q, want %q", out, want)
	}

	out = execCommand(t, cs, "PSUBSCRIBE")
	want = "-" + ErrWrongNumberOfArgs("psubscribe") + "\r\n"
	if string(out) != want {
		t.Fatalf("PSUBSCRIBE (no pattern) = %q, want %q", out, want)
	}

	out = execCommand(t, cs, "PUBLISH", "ch")
	want = "-" + ErrWrongNumberOfArgs("publish") + "\r\n"
	if string(out) != want {
		t.Fatalf("PUBLISH (no payload) = %q, want %q", out, want)
	}
}

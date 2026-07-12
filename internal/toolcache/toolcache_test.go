package toolcache

import (
	"testing"
	"time"
)

func TestToolKeySameInputsSameKey(t *testing.T) {
	k1, err := ToolKey("github.get_issue", `{"repo":"foo","number":42}`)
	if err != nil {
		t.Fatalf("ToolKey: %v", err)
	}
	k2, err := ToolKey("github.get_issue", `{"repo":"foo","number":42}`)
	if err != nil {
		t.Fatalf("ToolKey: %v", err)
	}
	if k1 != k2 {
		t.Fatalf("identical inputs produced different keys: %q vs %q", k1, k2)
	}
}

func TestToolKeyDifferentToolNameMisses(t *testing.T) {
	k1, err := ToolKey("github.get_issue", `{"repo":"foo","number":42}`)
	if err != nil {
		t.Fatalf("ToolKey: %v", err)
	}
	k2, err := ToolKey("jira.get_issue", `{"repo":"foo","number":42}`)
	if err != nil {
		t.Fatalf("ToolKey: %v", err)
	}
	if k1 == k2 {
		t.Fatal("different tool names produced the same key")
	}
}

func TestToolKeyDifferentArgValuesMisses(t *testing.T) {
	k1, err := ToolKey("github.get_issue", `{"repo":"foo","number":42}`)
	if err != nil {
		t.Fatalf("ToolKey: %v", err)
	}
	k2, err := ToolKey("github.get_issue", `{"repo":"foo","number":43}`)
	if err != nil {
		t.Fatalf("ToolKey: %v", err)
	}
	if k1 == k2 {
		t.Fatal("different arg values produced the same key")
	}
}

func TestToolKeyArgOrderIndependent(t *testing.T) {
	k1, err := ToolKey("github.get_issue", `{"repo":"foo","number":42}`)
	if err != nil {
		t.Fatalf("ToolKey: %v", err)
	}
	k2, err := ToolKey("github.get_issue", `{"number":42,"repo":"foo"}`)
	if err != nil {
		t.Fatalf("ToolKey: %v", err)
	}
	if k1 != k2 {
		t.Fatalf("same args in different JSON key order produced different keys: %q vs %q", k1, k2)
	}
}

func TestToolKeyInvalidJSON(t *testing.T) {
	if _, err := ToolKey("github.get_issue", "not json"); err == nil {
		t.Fatal("expected an error for invalid args JSON")
	}
}

func TestToolCacheSetGetRoundTrip(t *testing.T) {
	c := New()
	key, err := ToolKey("github.get_issue", `{"repo":"foo","number":42}`)
	if err != nil {
		t.Fatalf("ToolKey: %v", err)
	}

	if _, found := c.Get(key); found {
		t.Fatal("expected miss before Set")
	}

	c.Set(key, `{"title":"a bug"}`, 0)

	result, found := c.Get(key)
	if !found {
		t.Fatal("expected hit after Set")
	}
	if result != `{"title":"a bug"}` {
		t.Fatalf("result = %q, want %q", result, `{"title":"a bug"}`)
	}
}

func TestToolCacheTTLExpiry(t *testing.T) {
	c := New()
	c.Set("k", "v", 30*time.Millisecond)

	if _, found := c.Get("k"); !found {
		t.Fatal("expected hit before TTL expiry")
	}

	time.Sleep(60 * time.Millisecond)

	if _, found := c.Get("k"); found {
		t.Fatal("expected miss after TTL expiry")
	}
}

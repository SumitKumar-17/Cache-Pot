package semantic

import (
	"testing"
	"time"
)

func TestTemplateKeySameInputsSameKey(t *testing.T) {
	k1, err := TemplateKey("Hello {{name}}", `{"name":"Sumit"}`, "gpt-4")
	if err != nil {
		t.Fatalf("TemplateKey: %v", err)
	}
	k2, err := TemplateKey("Hello {{name}}", `{"name":"Sumit"}`, "gpt-4")
	if err != nil {
		t.Fatalf("TemplateKey: %v", err)
	}
	if k1 != k2 {
		t.Fatalf("identical inputs produced different keys: %q vs %q", k1, k2)
	}
}

func TestTemplateKeyDifferentTemplateMisses(t *testing.T) {
	k1, err := TemplateKey("Hello {{name}}", `{"name":"Sumit"}`, "gpt-4")
	if err != nil {
		t.Fatalf("TemplateKey: %v", err)
	}
	k2, err := TemplateKey("Hi {{name}}", `{"name":"Sumit"}`, "gpt-4")
	if err != nil {
		t.Fatalf("TemplateKey: %v", err)
	}
	if k1 == k2 {
		t.Fatal("different template text produced the same key")
	}
}

func TestTemplateKeyDifferentVariableValuesMisses(t *testing.T) {
	k1, err := TemplateKey("Hello {{name}}", `{"name":"Sumit"}`, "gpt-4")
	if err != nil {
		t.Fatalf("TemplateKey: %v", err)
	}
	k2, err := TemplateKey("Hello {{name}}", `{"name":"Someone Else"}`, "gpt-4")
	if err != nil {
		t.Fatalf("TemplateKey: %v", err)
	}
	if k1 == k2 {
		t.Fatal("different variable values produced the same key")
	}
}

func TestTemplateKeyVariableOrderIndependent(t *testing.T) {
	k1, err := TemplateKey("Hello {{name}}, {{lang}}", `{"name":"Sumit","lang":"Go"}`, "gpt-4")
	if err != nil {
		t.Fatalf("TemplateKey: %v", err)
	}
	k2, err := TemplateKey("Hello {{name}}, {{lang}}", `{"lang":"Go","name":"Sumit"}`, "gpt-4")
	if err != nil {
		t.Fatalf("TemplateKey: %v", err)
	}
	if k1 != k2 {
		t.Fatalf("same variables in different JSON key order produced different keys: %q vs %q", k1, k2)
	}
}

func TestTemplateKeyDifferentModelMisses(t *testing.T) {
	k1, err := TemplateKey("Hello {{name}}", `{"name":"Sumit"}`, "gpt-4")
	if err != nil {
		t.Fatalf("TemplateKey: %v", err)
	}
	k2, err := TemplateKey("Hello {{name}}", `{"name":"Sumit"}`, "claude")
	if err != nil {
		t.Fatalf("TemplateKey: %v", err)
	}
	if k1 == k2 {
		t.Fatal("different model produced the same key")
	}
}

func TestTemplateKeyInvalidJSON(t *testing.T) {
	if _, err := TemplateKey("Hello {{name}}", `not json`, "gpt-4"); err == nil {
		t.Fatal("expected an error for invalid variables JSON")
	}
}

func TestPromptCacheSetGetRoundTrip(t *testing.T) {
	p := NewPromptCache()
	key, err := TemplateKey("Hello {{name}}", `{"name":"Sumit"}`, "gpt-4")
	if err != nil {
		t.Fatalf("TemplateKey: %v", err)
	}

	if _, found, _ := p.Get(key); found {
		t.Fatal("expected miss before Set")
	}

	p.Set(key, "Hello Sumit!", 0, 0)

	resp, found, _ := p.Get(key)
	if !found {
		t.Fatal("expected hit after Set")
	}
	if resp != "Hello Sumit!" {
		t.Fatalf("response = %q, want %q", resp, "Hello Sumit!")
	}
}

func TestPromptCacheTTLExpiry(t *testing.T) {
	p := NewPromptCache()
	p.Set("k", "v", 30*time.Millisecond, 0)

	if _, found, _ := p.Get("k"); !found {
		t.Fatal("expected hit before TTL expiry")
	}

	time.Sleep(60 * time.Millisecond)

	if _, found, _ := p.Get("k"); found {
		t.Fatal("expected miss after TTL expiry")
	}
}

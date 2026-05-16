package main

import "testing"

func TestKeyHash(t *testing.T) {
	h := keyHash("secret")
	if len(h) != len("sha256:")+64 || h[:7] != "sha256:" {
		t.Fatalf("bad hash: %s", h)
	}
	if h != keyHash("secret") || h == keyHash("other") {
		t.Fatalf("hash is not stable/unique enough")
	}
}

func TestExtractToken(t *testing.T) {
	if got := extractToken(map[string]string{"Authorization": "Bearer abc"}); got != "abc" {
		t.Fatalf("got %q", got)
	}
	if got := extractToken(map[string]string{"X-API-Key": "def"}); got != "def" {
		t.Fatalf("got %q", got)
	}
}

func TestPGIdentEscapes(t *testing.T) {
	if got := pgIdent(`a"b`); got != `"a""b"` {
		t.Fatalf("got %q", got)
	}
}

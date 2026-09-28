package database

import (
	"bytes"
	"strings"
	"testing"
)

func TestRecoveryCodesHaveEntropyAndCanonicalDigests(t *testing.T) {
	seen := make(map[string]struct{})
	for i := 0; i < 100; i++ {
		code, digest, err := newRecoveryCode()
		if err != nil {
			t.Fatal(err)
		}
		if len(code) != 35 || strings.Count(code, "-") != 3 || len(digest) != 32 {
			t.Fatalf("invalid recovery format/digest: %s", code)
		}
		if _, ok := seen[code]; ok {
			t.Fatal("duplicate random code")
		}
		seen[code] = struct{}{}
		canonical, err := recoveryDigest(strings.ToLower(code))
		if err != nil || !bytes.Equal(digest, canonical) {
			t.Fatalf("recovery roundtrip invalid: %v", err)
		}
	}
	for _, bad := range []string{"", "ABC", "00000000-00000000-00000000-00000000", "!!!!!!!!-!!!!!!!!-!!!!!!!!-!!!!!!!!"} {
		if _, err := recoveryDigest(bad); err == nil {
			t.Fatalf("invalid code accepted: %q", bad)
		}
	}
}

func TestSessionTokenValidation(t *testing.T) {
	code, hashed, err := newToken()
	if err != nil {
		t.Fatal(err)
	}
	actual, err := tokenDigest(code)
	if err != nil || !bytes.Equal(actual, hashed) {
		t.Fatalf("token hash mismatch: %v", err)
	}
	if _, err := tokenDigest("bad-token"); err == nil {
		t.Fatal("malformed token accepted")
	}
}

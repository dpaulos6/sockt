package server

import "testing"

func TestAccountAttemptLimiter(t *testing.T) {
	limiter := newAttemptLimiter()
	for i := 0; i < 10; i++ {
		if !limiter.allow("Paulos") {
			t.Fatalf("legitimate attempt %d blocked", i)
		}
	}
	if limiter.allow("paulos") {
		t.Fatal("case variant bypassed limit")
	}
	if !limiter.allow("Miguel") {
		t.Fatal("another user's limit was affected")
	}
	limiter.reset("PAULOS")
	if !limiter.allow("paulos") {
		t.Fatal("success did not reset counter")
	}
}

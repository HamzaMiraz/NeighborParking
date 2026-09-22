package httpapi

import (
	"testing"
	"time"
)

func TestRateLimiter(t *testing.T) {
	l := newRateLimiter()
	if !l.allow("login:one", 2, time.Minute) {
		t.Fatal("first request denied")
	}
	if !l.allow("login:one", 2, time.Minute) {
		t.Fatal("second request denied")
	}
	if l.allow("login:one", 2, time.Minute) {
		t.Fatal("third request should be denied")
	}
	if !l.allow("login:two", 2, time.Minute) {
		t.Fatal("independent key denied")
	}
}

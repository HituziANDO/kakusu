//go:build !windows

package agent

import (
	"os"
	"sync"
	"testing"
	"time"
)

// startServe runs serve under a new HOME and waits for its socket. serve
// exits by itself on its idle timeout, so the test does not stop it.
func startServe(t *testing.T, ttl, maxKeyAge time.Duration, wallNow func() time.Time) {
	t.Helper()
	// Short path: macOS limits Unix socket paths to 104 bytes.
	home, err := os.MkdirTemp("", "kk")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	t.Setenv("HOME", home)

	go serve(ttl, maxKeyAge, wallNow)
	for i := 0; i < 100; i++ {
		if c, err := Dial(); err == nil {
			c.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("agent did not start")
}

// fakeWall is a wall clock that only the test moves. Moving it forward plays
// a system sleep: the wall clock advances while the monotonic clock, which the
// TTL runs on, does not.
type fakeWall struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeWall() *fakeWall { return &fakeWall{t: wallClock()} }

func (w *fakeWall) now() time.Time {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.t
}

func (w *fakeWall) advance(d time.Duration) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.t = w.t.Add(d)
}

func mustSetKey(t *testing.T) {
	t.Helper()
	if err := SetKey([]byte("key"), []byte("salt")); err != nil {
		t.Fatal(err)
	}
}

func mustGetKey(t *testing.T) []byte {
	t.Helper()
	key, _, err := GetKey()
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func mustQueryStatus(t *testing.T) *Response {
	t.Helper()
	resp, err := QueryStatus()
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestKeyExpiresMaxKeyAgeAfterSetKeyOnWallClock(t *testing.T) {
	wall := newFakeWall()
	startServe(t, time.Hour, 8*time.Hour, wall.now)
	mustSetKey(t)

	wall.advance(8*time.Hour - time.Minute)
	if mustGetKey(t) == nil {
		t.Fatal("GET_KEY 1m before maxKeyAge: NO_KEY; want the key")
	}
	// That GET_KEY extended the TTL to 1h, but only 1m remains until maxKeyAge.
	if resp := mustQueryStatus(t); !resp.HasKey || resp.RemainingSeconds != 60 {
		t.Errorf("STATUS 1m before maxKeyAge: HasKey %v, RemainingSeconds %d; want true, 60", resp.HasKey, resp.RemainingSeconds)
	}

	wall.advance(time.Minute + time.Second)
	if resp := mustQueryStatus(t); resp.HasKey {
		t.Errorf("STATUS 1s after maxKeyAge: HasKey true, RemainingSeconds %d; want false", resp.RemainingSeconds)
	}
	if mustGetKey(t) != nil {
		t.Error("GET_KEY 1s after maxKeyAge: got the key; want NO_KEY")
	}
}

func TestSetKeyRestartsMaxKeyAge(t *testing.T) {
	wall := newFakeWall()
	startServe(t, time.Hour, 8*time.Hour, wall.now)
	mustSetKey(t)
	wall.advance(5 * time.Hour)
	mustSetKey(t)

	// 9h after the first SET_KEY and 4h after the last. STATUS checks that the
	// key is still valid after the GET_KEY extension, not only by the TTL that
	// SET_KEY set.
	wall.advance(4 * time.Hour)
	if mustGetKey(t) == nil {
		t.Fatal("GET_KEY 4h after the last SET_KEY: NO_KEY; want the key")
	}
	if !mustQueryStatus(t).HasKey {
		t.Error("STATUS 4h after the last SET_KEY: HasKey false; want true")
	}

	wall.advance(4*time.Hour + time.Second)
	if mustGetKey(t) != nil {
		t.Error("GET_KEY 8h1s after the last SET_KEY: got the key; want NO_KEY")
	}
}

func TestGetKeyExtendsTTL(t *testing.T) {
	// The wall clock stands still, so maxKeyAge stays out of the way.
	startServe(t, time.Second, 8*time.Hour, newFakeWall().now)
	mustSetKey(t)

	// GET_KEY every 100ms keeps the key past twice the TTL. This fails only
	// if two GET_KEYs are more than the TTL apart.
	for start := time.Now(); time.Since(start) < 2200*time.Millisecond; time.Sleep(100 * time.Millisecond) {
		if mustGetKey(t) == nil {
			t.Fatalf("GET_KEY %v after SET_KEY: NO_KEY; want the key", time.Since(start))
		}
	}
	// With no GET_KEY for longer than the TTL, the key expires.
	time.Sleep(1500 * time.Millisecond)
	if mustGetKey(t) != nil {
		t.Error("GET_KEY 1.5s after the previous one: got the key; want NO_KEY")
	}
}

func TestServeClampsTTLToMaxKeyAge(t *testing.T) {
	startServe(t, 8*time.Hour+time.Minute, 8*time.Hour, newFakeWall().now)
	if resp := mustQueryStatus(t); resp.TTLSeconds != 8*60*60 {
		t.Errorf("TTLSeconds = %d; want %d", resp.TTLSeconds, 8*60*60)
	}
}

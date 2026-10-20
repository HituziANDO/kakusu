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

func TestGetKeyStopsExtendingAtMaxKeyAge(t *testing.T) {
	startServe(t, 300*time.Millisecond, time.Second, wallClock)
	if err := SetKey([]byte("key"), []byte("salt")); err != nil {
		t.Fatal(err)
	}
	start := time.Now()

	// GET_KEY more often than the TTL keeps extending it until maxKeyAge.
	for time.Since(start) < 1200*time.Millisecond {
		key, _, err := GetKey()
		if err != nil {
			t.Fatal(err)
		}
		if elapsed := time.Since(start); key == nil && elapsed < 850*time.Millisecond {
			t.Fatalf("GET_KEY %v after SET_KEY: NO_KEY; want the key", elapsed)
		}
		time.Sleep(100 * time.Millisecond)
	}
	key, _, err := GetKey()
	if err != nil {
		t.Fatal(err)
	}
	if key != nil {
		t.Fatalf("GET_KEY %v after SET_KEY: got the key; want NO_KEY", time.Since(start))
	}
}

func TestServeClampsTTLToMaxKeyAge(t *testing.T) {
	startServe(t, time.Hour, 3*time.Second, wallClock)
	if err := SetKey([]byte("key"), []byte("salt")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1600 * time.Millisecond)
	if key, _, err := GetKey(); err != nil || key == nil {
		t.Fatalf("GET_KEY: key %v, err %v; want the key", key, err)
	}

	resp, err := QueryStatus()
	if err != nil {
		t.Fatal(err)
	}
	if resp.TTLSeconds != 3 {
		t.Errorf("TTLSeconds = %d; want 3", resp.TTLSeconds)
	}
	// About 1.4s remain until maxKeyAge; the GET_KEY extension alone would
	// leave about 3s.
	if !resp.HasKey || resp.RemainingSeconds != 1 {
		t.Errorf("HasKey %v, RemainingSeconds %d; want true, 1", resp.HasKey, resp.RemainingSeconds)
	}
}

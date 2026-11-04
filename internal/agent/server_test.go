//go:build !windows

package agent

import (
	"os"
	"strings"
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
// a system sleep: the wall clock advances while the monotonic clock, which
// keyTimer runs on, does not. It starts years away from the real clock, so
// that a check that reads the real clock instead of it fails.
type fakeWall struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeWall() *fakeWall { return &fakeWall{t: time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)} }

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

// Sub between two times that both carry a monotonic reading uses the monotonic
// clock, which stops while the system sleeps. Time.String shows that reading
// as "m=".
func TestWallClockHasNoMonotonicReading(t *testing.T) {
	if s := wallClock().String(); strings.Contains(s, " m=") {
		t.Errorf("wallClock() = %s; want no monotonic reading", s)
	}
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

// The TTL is 1h so that keyTimer, which runs on the monotonic clock, does not
// fire during the test: only the wall clock can expire the key. STATUS comes
// before GET_KEY, because a GET_KEY extends the TTL, and one after the TTL
// clears the key.
func TestKeyExpiresTTLAfterSetKeyOnWallClock(t *testing.T) {
	wall := newFakeWall()
	startServe(t, time.Hour, 8*time.Hour, wall.now)
	mustSetKey(t)

	wall.advance(time.Hour - time.Second)
	if !mustQueryStatus(t).HasKey {
		t.Fatal("STATUS 1s before the TTL after SET_KEY: HasKey false; want true")
	}

	wall.advance(2 * time.Second)
	if resp := mustQueryStatus(t); resp.HasKey {
		t.Errorf("STATUS 1s after the TTL after SET_KEY: HasKey true, RemainingSeconds %d; want false", resp.RemainingSeconds)
	}
	if mustGetKey(t) != nil {
		t.Error("GET_KEY 1s after the TTL after SET_KEY: got the key; want NO_KEY")
	}
}

func TestKeyExpiresTTLAfterGetKeyOnWallClock(t *testing.T) {
	wall := newFakeWall()
	startServe(t, time.Hour, 8*time.Hour, wall.now)
	mustSetKey(t)
	wall.advance(30 * time.Minute)
	if mustGetKey(t) == nil {
		t.Fatal("GET_KEY 30m after SET_KEY: NO_KEY; want the key")
	}

	// 1s before the TTL from that GET_KEY, and past the TTL from SET_KEY.
	wall.advance(time.Hour - time.Second)
	if !mustQueryStatus(t).HasKey {
		t.Fatal("STATUS 1s before the TTL after GET_KEY: HasKey false; want true")
	}

	wall.advance(2 * time.Second)
	if resp := mustQueryStatus(t); resp.HasKey {
		t.Errorf("STATUS 1s after the TTL after GET_KEY: HasKey true, RemainingSeconds %d; want false", resp.RemainingSeconds)
	}
	if mustGetKey(t) != nil {
		t.Error("GET_KEY 1s after the TTL after GET_KEY: got the key; want NO_KEY")
	}
}

// Moving the wall clock back must not leave more than the TTL: the monotonic
// clock, which does not move back, still bounds it.
func TestWallClockMovingBackDoesNotExtendTTL(t *testing.T) {
	for _, last := range []string{"SET_KEY", "GET_KEY"} {
		t.Run(last, func(t *testing.T) {
			wall := newFakeWall()
			startServe(t, time.Hour, 8*time.Hour, wall.now)
			mustSetKey(t)
			if last == "GET_KEY" && mustGetKey(t) == nil {
				t.Fatal("GET_KEY right after SET_KEY: NO_KEY; want the key")
			}

			wall.advance(-30 * time.Minute)
			if resp := mustQueryStatus(t); !resp.HasKey || resp.RemainingSeconds > resp.TTLSeconds {
				t.Errorf("STATUS after the wall clock moved back 30m: HasKey %v, RemainingSeconds %d; want true, at most TTLSeconds %d", resp.HasKey, resp.RemainingSeconds, resp.TTLSeconds)
			}
		})
	}
}

func TestKeyExpiresMaxKeyAgeAfterSetKeyOnWallClock(t *testing.T) {
	wall := newFakeWall()
	startServe(t, 8*time.Hour, 8*time.Hour, wall.now)
	mustSetKey(t)

	wall.advance(8*time.Hour - time.Minute)
	if mustGetKey(t) == nil {
		t.Fatal("GET_KEY 1m before maxKeyAge: NO_KEY; want the key")
	}
	// The deadline counts from SET_KEY, and that GET_KEY did not move it.
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
	startServe(t, 8*time.Hour, 8*time.Hour, wall.now)
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
	wall := newFakeWall()
	startServe(t, time.Hour, 8*time.Hour, wall.now)
	mustSetKey(t)

	// GET_KEY every 50m keeps the key past twice the TTL.
	for elapsed := 50 * time.Minute; elapsed <= 150*time.Minute; elapsed += 50 * time.Minute {
		wall.advance(50 * time.Minute)
		if mustGetKey(t) == nil {
			t.Fatalf("GET_KEY %v after SET_KEY: NO_KEY; want the key", elapsed)
		}
	}
	// With no GET_KEY for longer than the TTL, the key expires.
	wall.advance(time.Hour + time.Second)
	if mustGetKey(t) != nil {
		t.Error("GET_KEY 1h1s after the previous one: got the key; want NO_KEY")
	}
}

func TestServeClampsTTLToMaxKeyAge(t *testing.T) {
	startServe(t, 8*time.Hour+time.Minute, 8*time.Hour, newFakeWall().now)
	if resp := mustQueryStatus(t); resp.TTLSeconds != 8*60*60 {
		t.Errorf("TTLSeconds = %d; want %d", resp.TTLSeconds, 8*60*60)
	}
}

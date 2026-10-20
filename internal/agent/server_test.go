//go:build !windows

package agent

import (
	"os"
	"testing"
	"time"
)

// startServe runs Serve under a new HOME and waits for its socket. Serve
// exits by itself on its idle timeout, so the test does not stop it.
func startServe(t *testing.T, ttl, maxKeyAge time.Duration) {
	t.Helper()
	// Short path: macOS limits Unix socket paths to 104 bytes.
	home, err := os.MkdirTemp("", "kk")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	t.Setenv("HOME", home)

	go Serve(ttl, maxKeyAge)
	for i := 0; i < 100; i++ {
		if c, err := Dial(); err == nil {
			c.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("agent did not start")
}

func TestGetKeyStopsExtendingAtMaxKeyAge(t *testing.T) {
	startServe(t, 300*time.Millisecond, time.Second)
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
	startServe(t, time.Hour, 3*time.Second)
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

package agent

import (
	"encoding/base64"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/HituziANDO/kakusu/internal/config"
	"github.com/HituziANDO/kakusu/internal/platform"
)

func stopTimer(t *time.Timer) {
	if !t.Stop() {
		select {
		case <-t.C:
		default:
		}
	}
}

func Serve(ttlDur, maxKeyAge time.Duration) {
	serve(ttlDur, maxKeyAge, wallClock)
}

// wallClock returns the current time without its monotonic clock reading, so
// that comparisons and durations between such times use the wall clock. The
// monotonic clock stops while the system sleeps (on darwin and Linux); the
// wall clock does not.
func wallClock() time.Time {
	return time.Now().Round(0)
}

// serve is Serve with the clock for the maxKeyAge deadline passed in, so that
// tests can move it forward as a system sleep would.
func serve(ttlDur, maxKeyAge time.Duration, wallNow func() time.Time) {
	if ttlDur > maxKeyAge {
		ttlDur = maxKeyAge
	}
	sockPath := config.AgentSocketPath()

	if conn, err := net.DialTimeout("unix", sockPath, 500*time.Millisecond); err == nil {
		conn.Close()
		return
	}

	os.Remove(sockPath)
	os.MkdirAll(config.KakusuHome(), 0700)

	listener, err := net.Listen("unix", sockPath)
	if err != nil {
		return
	}
	os.Chmod(sockPath, 0600)

	pidPath := config.AgentPIDPath()
	os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())), 0600)

	connCh := make(chan net.Conn, 1)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				close(connCh)
				return
			}
			connCh <- conn
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	platform.IgnoreHUP()

	var cachedKey, cachedSalt []byte
	// expiresAt slides on GET_KEY. keyDeadline is set on SET_KEY and is on the
	// wall clock, so it passes even if the system sleeps through it.
	var expiresAt, keyDeadline time.Time

	// keyRemaining is how long the cached key stays valid: until the TTL
	// expires or keyDeadline passes, whichever comes first. Every request
	// checks it, because keyTimer can fire late after a sleep.
	keyRemaining := func() time.Duration {
		r := time.Until(expiresAt)
		if d := keyDeadline.Sub(wallNow()); d < r {
			r = d
		}
		return r
	}

	clearKey := func() {
		for i := range cachedKey {
			cachedKey[i] = 0
		}
		cachedKey = nil
		cachedSalt = nil
		expiresAt = time.Time{}
	}

	keyTimer := time.NewTimer(0)
	stopTimer(keyTimer)

	idleTimer := time.NewTimer(ttlDur * 2)

	for {
		select {
		case conn, ok := <-connCh:
			if !ok {
				goto done
			}

			var req Request
			if err := RecvMsg(conn, &req); err != nil {
				conn.Close()
				continue
			}

			var resp Response
			switch req.Type {
			case "GET_KEY":
				if cachedKey != nil && keyRemaining() > 0 {
					resp.Status = "OK"
					resp.Key = base64.StdEncoding.EncodeToString(cachedKey)
					resp.Salt = base64.StdEncoding.EncodeToString(cachedSalt)
					expiresAt = time.Now().Add(ttlDur)
					stopTimer(keyTimer)
					keyTimer.Reset(keyRemaining())
				} else {
					if cachedKey != nil {
						clearKey()
						stopTimer(keyTimer)
						stopTimer(idleTimer)
						idleTimer.Reset(ttlDur * 2)
					}
					resp.Status = "NO_KEY"
				}

			case "SET_KEY":
				clearKey()
				cachedKey, _ = base64.StdEncoding.DecodeString(req.Key)
				cachedSalt, _ = base64.StdEncoding.DecodeString(req.Salt)
				expiresAt = time.Now().Add(ttlDur)
				keyDeadline = wallNow().Add(maxKeyAge)
				stopTimer(keyTimer)
				keyTimer.Reset(ttlDur)
				stopTimer(idleTimer)
				resp.Status = "OK"

			case "CLEAR_KEY":
				clearKey()
				stopTimer(keyTimer)
				stopTimer(idleTimer)
				idleTimer.Reset(ttlDur * 2)
				resp.Status = "OK"

			case "STATUS":
				resp.Status = "OK"
				remaining := keyRemaining()
				resp.HasKey = cachedKey != nil && remaining > 0
				resp.TTLSeconds = int(ttlDur.Seconds())
				if resp.HasKey {
					resp.RemainingSeconds = int(remaining.Seconds())
				}

			default:
				resp.Status = "ERROR"
			}

			SendMsg(conn, &resp)
			conn.Close()

		case <-keyTimer.C:
			clearKey()
			stopTimer(idleTimer)
			idleTimer.Reset(ttlDur * 2)

		case <-idleTimer.C:
			goto done

		case <-sigCh:
			goto done
		}
	}

done:
	clearKey()
	listener.Close()
	os.Remove(sockPath)
	os.Remove(pidPath)
}

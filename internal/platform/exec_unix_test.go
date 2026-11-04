//go:build !windows

package platform

import (
	"slices"
	"testing"
)

func TestChildArgsKeepsNameAsTyped(t *testing.T) {
	args := []string{"tool", "-v"}
	if got := ChildArgs("/usr/local/bin/tool", args); !slices.Equal(got, args) {
		t.Errorf("ChildArgs = %q; want %q, as ExecRun passes them", got, args)
	}
}

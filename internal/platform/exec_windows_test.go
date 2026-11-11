//go:build windows

package platform

import (
	"slices"
	"testing"
)

func TestChildArgsUsesResolvedPath(t *testing.T) {
	got := ChildArgs(`C:\tools\tool.bat`, []string{"tool", "-v"})
	if want := []string{`C:\tools\tool.bat`, "-v"}; !slices.Equal(got, want) {
		t.Errorf("ChildArgs = %q; want %q, as ExecRun passes them", got, want)
	}
}

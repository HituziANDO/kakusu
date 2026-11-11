package cli

import "testing"

func TestChooseRunMode(t *testing.T) {
	// Rows with sameFile set and only one stream a terminal are left out: one
	// file is a terminal for both streams or for neither.
	for _, tc := range []struct {
		noMasking, stdoutTerminal, stderrTerminal, sameFile bool
		want                                                runMode
	}{
		{false, true, true, true, runExec},
		{false, true, true, false, runExec},
		{false, true, false, false, runConceal},
		{false, false, true, false, runConceal},
		{false, false, false, false, runConceal},
		{false, false, false, true, runConcealShared},
		{true, true, true, true, runExec},
		{true, true, true, false, runExec},
		{true, true, false, false, runExec},
		{true, false, true, false, runExec},
		{true, false, false, false, runExec},
		{true, false, false, true, runExec},
	} {
		if got := chooseRunMode(tc.noMasking, tc.stdoutTerminal, tc.stderrTerminal, tc.sameFile); got != tc.want {
			t.Errorf("chooseRunMode(noMasking %v, stdoutTerminal %v, stderrTerminal %v, sameFile %v) = %d; want %d",
				tc.noMasking, tc.stdoutTerminal, tc.stderrTerminal, tc.sameFile, got, tc.want)
		}
	}
}

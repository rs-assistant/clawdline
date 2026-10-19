//go:build !unix

package cloud

import "testing"

func wireBenchCPU(t *testing.T) int64 {
	t.Helper()
	t.Skip("process CPU sampling for this opt-in baseline requires Unix")
	return 0
}

func wireBenchCPUParts(t *testing.T) (int64, int64) {
	t.Helper()
	t.Skip("isolated process CPU timing requires Unix")
	return 0, 0
}

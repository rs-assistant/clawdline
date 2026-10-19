//go:build !unix

package cloud

import "testing"

func wireBenchCPU(t *testing.T) int64 {
	t.Helper()
	t.Skip("process CPU sampling for this opt-in baseline requires Unix")
	return 0
}

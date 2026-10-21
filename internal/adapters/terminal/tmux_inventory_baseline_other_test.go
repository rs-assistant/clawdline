//go:build !darwin && !linux

package terminal

import "testing"

func TestTmuxInventoryBaseline(t *testing.T) {
	t.Skip("local inventory baseline requires Darwin/Linux self CPU accounting; no measurement")
}

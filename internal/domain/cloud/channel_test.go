package cloud

import "testing"

// TestChannelSegmentIsEncodeURIComponent. A tmux pane is `%19`, and the same
// function names both a relay channel's segment and a local route's path
// segment, exactly as the Swift bridge uses it.
func TestChannelSegmentIsEncodeURIComponent(t *testing.T) {
	cases := map[string]string{
		"%19":                     "%2519",
		"/Users/sean/code":        "%2FUsers%2Fsean%2Fcode",
		"mac-01":                  "mac-01",
		"a b":                     "a%20b",
		"~_.!*'()-":               "~_.!*'()-",
		"__clawdline_machine__":   "__clawdline_machine__",
		"session|with|separators": "session%7Cwith%7Cseparators",
		"\x00\xff":                "%00%FF",
	}
	for in, want := range cases {
		if got := ChannelSegment(in); got != want {
			t.Fatalf("%q encoded to %q, wanted %q", in, got, want)
		}
	}
}

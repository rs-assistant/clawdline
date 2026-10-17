package cloud

import "strings"

// ChannelSegment is the hosted console's `channelSegment`: percent-encoding
// with the unreserved set JavaScript's encodeURIComponent leaves alone.
//
// It is used for two different things on purpose, exactly as the Swift bridge
// uses it: the segments of a relay channel, and the segments of a local route's
// path. A tmux pane is `%19`, and a path that carried that raw would be a
// different path. The publisher, the command answerer and the viewer client
// all name channels with this one function, so the two ends cannot drift.
func ChannelSegment(value string) string {
	const safe = "-_.!~*'()"
	var out strings.Builder
	for i := 0; i < len(value); i++ {
		c := value[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9',
			strings.IndexByte(safe, c) >= 0:
			out.WriteByte(c)
		default:
			const hex = "0123456789ABCDEF"
			out.WriteByte('%')
			out.WriteByte(hex[c>>4])
			out.WriteByte(hex[c&0x0f])
		}
	}
	return out.String()
}

package documents

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"strings"
)

// MaximumImageSide and MaximumImagePixels bound decoded screenshot memory even
// when a highly compressed image fits inside the existing document byte cap.
const MaximumImageSide = 8192
const MaximumImagePixels = 16 * 1024 * 1024

// MaximumReportImagePixels bounds the total decoded images retained in one report.
const MaximumReportImagePixels = 64 * 1024 * 1024

func validInlineImage(src string) bool {
	return inlineImagePixels(src, MaximumReportImagePixels) > 0
}

func inlineImagePixels(src string, remaining int64) int64 {
	format := ""
	switch {
	case strings.HasPrefix(src, "data:image/png;base64,"):
		format = "png"
	case strings.HasPrefix(src, "data:image/jpeg;base64,"):
		format = "jpeg"
	default:
		return 0
	}
	encoded := src[strings.IndexByte(src, ',')+1:]
	if len(src) > MaximumBytes || strings.ContainsAny(encoded, "\r\n") {
		return 0
	}
	body, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil || len(body) == 0 || base64.StdEncoding.EncodeToString(body) != encoded {
		return 0
	}
	if format == "png" && !staticPNG(body) {
		return 0
	}
	config, actual, err := image.DecodeConfig(bytes.NewReader(body))
	if err != nil || actual != format || config.Width <= 0 || config.Height <= 0 || config.Width > MaximumImageSide || config.Height > MaximumImageSide || int64(config.Width)*int64(config.Height) > MaximumImagePixels {
		return 0
	}
	pixels := int64(config.Width) * int64(config.Height)
	if pixels > remaining {
		return 0
	}
	// Decode only after dimensions have passed: malformed/truncated pixels are
	// omitted too, without allocating an attacker-declared giant raster.
	_, actual, err = image.Decode(bytes.NewReader(body))
	if err != nil || actual != format {
		return 0
	}
	return pixels
}

// Animated PNG can allocate many frames beyond the IHDR pixel count. Reports
// accept static screenshots only, and stop exactly at the PNG end marker.
func staticPNG(body []byte) bool {
	if len(body) < 8 || !bytes.Equal(body[:8], []byte{137, 80, 78, 71, 13, 10, 26, 10}) {
		return false
	}
	for offset := 8; offset+12 <= len(body); {
		length := int64(binary.BigEndian.Uint32(body[offset : offset+4]))
		end := int64(offset) + 12 + length
		if end > int64(len(body)) {
			return false
		}
		switch string(body[offset+4 : offset+8]) {
		case "acTL", "fcTL", "fdAT":
			return false
		case "IEND":
			return length == 0 && end == int64(len(body))
		}
		offset = int(end)
	}
	return false
}

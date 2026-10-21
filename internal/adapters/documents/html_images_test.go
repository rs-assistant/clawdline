package documents

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"
)

func inlineImageFixture(t *testing.T, format string) string {
	t.Helper()
	var b bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 3, 2))
	img.Set(0, 0, color.RGBA{R: 240, A: 255})
	var err error
	if format == "png" {
		err = png.Encode(&b, img)
	} else {
		err = jpeg.Encode(&b, img, nil)
	}
	if err != nil {
		t.Fatal(err)
	}
	return "data:image/" + format + ";base64," + base64.StdEncoding.EncodeToString(b.Bytes())
}

func TestHTMLInlineScreenshotsSurviveWithoutActiveAttributes(t *testing.T) {
	for _, format := range []string{"png", "jpeg"} {
		t.Run(format, func(t *testing.T) {
			src := inlineImageFixture(t, format)
			body := string(sanitizeHTML([]byte(`<figure><img src="` + src + `" alt="Screenshot" width="3" onerror="alert(1)" srcset="https://evil.invalid 2x" style="max-width:100%;background:url(https://evil.invalid)"><figcaption>Evidence</figcaption></figure>`)))
			for _, want := range []string{`<img`, `src="` + src + `"`, `alt="Screenshot"`, `<figcaption>Evidence</figcaption>`, `max-width: 100%`} {
				if !strings.Contains(body, want) {
					t.Errorf("missing %q in %s", want, body)
				}
			}
			for _, bad := range []string{"onerror", "srcset", "evil.invalid"} {
				if strings.Contains(body, bad) {
					t.Errorf("kept %s in %s", bad, body)
				}
			}
		})
	}
}

func TestHTMLInlineScreenshotsRejectOtherSourcesAndMalformedImages(t *testing.T) {
	good := inlineImageFixture(t, "png")
	encoded := strings.SplitN(good, ",", 2)[1]
	for _, src := range []string{
		"https://evil.invalid/a.png", "//evil.invalid/a.png", "file:///tmp/a.png", "blob:https://evil.invalid/a", "data:image/svg+xml;base64,PHN2Zy8+", "data:image/gif;base64,R0lGODlh", "data:image/webp;base64,UklGRg==",
		"DATA:image/png;base64," + encoded, "data:image/png;charset=utf-8;base64," + encoded, "data:image/png;base64, " + encoded,
		"data:image/png;base64," + encoded[:4] + "\n" + encoded[4:], "data:image/png;base64,%%%=", "data:image/png;base64,", "data:image/png;base64,AB==",
		"data:image/jpeg;base64," + encoded, "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("<svg onload='alert(1)'/>")),
		"data:image/png;base64," + encoded[:len(encoded)-4], good + "?query", good + "#fragment",
	} {
		t.Run(src[:min(len(src), 60)], func(t *testing.T) {
			if validInlineImage(src) {
				t.Fatal("accepted invalid image")
			}
			body := string(sanitizeHTML([]byte(`<p>before</p><img src="` + src + `"><p>after</p>`)))
			if strings.Contains(body, "<img") || !strings.Contains(body, "before") || !strings.Contains(body, "after") {
				t.Fatalf("unexpected output %s", body)
			}
		})
	}
}

func oversizedPNG(t *testing.T, width, height uint32) string {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(strings.SplitN(inlineImageFixture(t, "png"), ",", 2)[1])
	if err != nil {
		t.Fatal(err)
	}
	binary.BigEndian.PutUint32(raw[16:20], width)
	binary.BigEndian.PutUint32(raw[20:24], height)
	binary.BigEndian.PutUint32(raw[29:33], crc32.ChecksumIEEE(raw[12:29]))
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(raw)
}

func TestHTMLInlineScreenshotsBoundDecodedSizeBeforePixelAllocation(t *testing.T) {
	for _, src := range []string{oversizedPNG(t, MaximumImageSide+1, 1), oversizedPNG(t, 4097, 4096), oversizedPNG(t, 0, 1), oversizedPNG(t, 1, 0), oversizedPNG(t, ^uint32(0), ^uint32(0))} {
		if validInlineImage(src) {
			t.Fatal("accepted oversized or empty image")
		}
		if body := string(sanitizeHTML([]byte(`<img src="` + src + `">`))); strings.Contains(body, "<img") {
			t.Fatal("retained oversized image")
		}
	}
}

func TestHTMLInlineScreenshotsCountRepeatedImagesAgainstReportBudget(t *testing.T) {
	src := inlineImageFixture(t, "png") // Six decoded pixels, even when repeated.
	input := []byte(strings.Repeat(`<img src="`+src+`">`, 3) + `<p>still readable</p>`)
	for _, test := range []struct {
		budget int64
		want   int
	}{{0, 0}, {5, 0}, {6, 1}, {12, 2}, {18, 3}} {
		body := string(sanitizeHTMLWithImageBudget(input, test.budget))
		if got := strings.Count(body, "<img"); got != test.want {
			t.Errorf("budget=%d: images=%d want=%d", test.budget, got, test.want)
		}
		if !strings.Contains(body, "still readable") {
			t.Fatal("lost report text")
		}
	}
}

func TestHTMLInlineScreenshotsRejectAnimatedPNG(t *testing.T) {
	raw, err := base64.StdEncoding.DecodeString(strings.SplitN(inlineImageFixture(t, "png"), ",", 2)[1])
	if err != nil {
		t.Fatal(err)
	}
	// An APNG animation-control chunk is ignored by ordinary PNG decoders;
	// browsers would instead retain multiple frames behind one IHDR budget.
	var chunk bytes.Buffer
	_ = binary.Write(&chunk, binary.BigEndian, uint32(8))
	chunk.WriteString("acTL")
	_ = binary.Write(&chunk, binary.BigEndian, uint32(2))
	_ = binary.Write(&chunk, binary.BigEndian, uint32(0))
	_ = binary.Write(&chunk, binary.BigEndian, crc32.ChecksumIEEE(chunk.Bytes()[4:]))
	animated := append(append(append([]byte{}, raw[:33]...), chunk.Bytes()...), raw[33:]...)
	for _, body := range [][]byte{animated, append(append([]byte{}, raw...), byte(0))} {
		if validInlineImage("data:image/png;base64," + base64.StdEncoding.EncodeToString(body)) {
			t.Fatal("accepted animated or trailing PNG")
		}
	}
}

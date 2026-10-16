package qrcode

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// testdata/qr-ts.json is what the console's encoder (qr.ts), whose output was
// read back by an independent decoder when it shipped, draws for each text.
// The port must draw the same modules, mask and level for the same text, so a
// phone that scans the console's code scans this one.
func TestEncodeMatchesTheConsoleEncoder(t *testing.T) {
	raw, err := os.ReadFile("testdata/qr-ts.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Text    string   `json:"text"`
		Version int      `json:"version"`
		ECC     string   `json:"ecc"`
		Mask    int      `json:"mask"`
		Rows    []string `json:"rows"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("no cases")
	}
	for _, want := range cases {
		got, err := Encode(want.Text)
		if err != nil {
			t.Fatalf("%.20q: %v", want.Text, err)
		}
		if got.Version != want.Version || string(got.ECC) != want.ECC || got.Mask != want.Mask {
			t.Errorf("%.20q: version %d ecc %c mask %d, want %d %s %d",
				want.Text, got.Version, got.ECC, got.Mask, want.Version, want.ECC, want.Mask)
			continue
		}
		for y, row := range want.Rows {
			var b strings.Builder
			for x := 0; x < got.Size; x++ {
				if got.Dark[y*got.Size+x] {
					b.WriteByte('#')
				} else {
					b.WriteByte('.')
				}
			}
			if b.String() != row {
				t.Errorf("%.20q row %d:\n got %s\nwant %s", want.Text, y, b.String(), row)
				break
			}
		}
	}
}

// A pairing link fits an 80-column terminal: half blocks make one character
// per module across, quiet zone included.
func TestTerminalFitsEightyColumns(t *testing.T) {
	link := "https://app.clawdline.com/#pair=" + strings.Repeat("A", 300)
	code, err := Encode(link)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimRight(code.Terminal(), "\n"), "\n") {
		if n := len([]rune(line)); n > 80 {
			t.Fatalf("a line is %d columns", n)
		}
	}
}

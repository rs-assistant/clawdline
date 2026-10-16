// Package qrcode encodes one byte-mode QR code and draws it for a terminal.
//
// It is a port of the console's encoder (web/console/src/pages/settings/window/qr.ts)
// so that a machine with no screen can show the same pairing link as a code an
// iPhone's Home Screen app scans: a link printed as text can only be opened in
// Safari, whose storage the Home Screen app does not share. The construction
// follows ISO/IEC 18004 in the same order as qr.ts, and the test holds the two
// to the same modules for the same text.
package qrcode

import (
	"fmt"
	"math"
	"strings"
)

// Code is one encoded symbol: Size modules on a side, Dark[y*Size+x] set for a
// dark module. The quiet zone is not included.
type Code struct {
	Version int
	Size    int
	ECC     byte
	Mask    int
	Dark    []bool
}

var eccOrder = []byte{'L', 'M', 'Q', 'H'}

var formatBits = map[byte]int{'L': 1, 'M': 0, 'Q': 3, 'H': 2}

// ISO/IEC 18004 Table 9, indexed by version (index 0 unused).
var eccPerBlock = map[byte][]int{
	'L': {0, 7, 10, 15, 20, 26, 18, 20, 24, 30, 18, 20, 24, 26, 30, 22, 24, 28, 30, 28, 28, 28, 28, 30, 30, 26, 28, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30},
	'M': {0, 10, 16, 26, 18, 24, 16, 18, 22, 22, 26, 30, 22, 22, 24, 24, 28, 28, 26, 26, 26, 26, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28},
	'Q': {0, 13, 22, 18, 26, 18, 24, 18, 22, 20, 24, 28, 26, 24, 20, 30, 24, 28, 28, 26, 30, 28, 30, 30, 30, 30, 28, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30},
	'H': {0, 17, 28, 22, 16, 22, 28, 26, 26, 24, 28, 24, 28, 22, 24, 24, 30, 28, 28, 26, 28, 30, 24, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30},
}

var blocks = map[byte][]int{
	'L': {0, 1, 1, 1, 1, 1, 2, 2, 2, 2, 4, 4, 4, 4, 4, 6, 6, 6, 6, 7, 8, 8, 9, 9, 10, 12, 12, 12, 13, 14, 15, 16, 17, 18, 19, 19, 20, 21, 22, 24, 25},
	'M': {0, 1, 1, 1, 2, 2, 4, 4, 4, 5, 5, 5, 8, 9, 9, 10, 10, 11, 13, 14, 16, 17, 17, 18, 20, 21, 23, 25, 26, 28, 29, 31, 33, 35, 37, 38, 40, 43, 45, 47, 49},
	'Q': {0, 1, 1, 2, 2, 4, 4, 6, 6, 8, 8, 8, 10, 12, 16, 12, 17, 16, 18, 21, 20, 23, 23, 25, 27, 29, 34, 34, 35, 38, 40, 43, 45, 48, 51, 53, 56, 59, 62, 65, 68},
	'H': {0, 1, 1, 2, 4, 4, 4, 5, 6, 8, 8, 11, 11, 16, 16, 18, 16, 19, 21, 25, 25, 25, 34, 30, 32, 35, 37, 40, 42, 45, 48, 51, 54, 57, 60, 63, 66, 70, 74, 77, 81},
}

func rawDataModules(version int) int {
	result := (16*version+128)*version + 64
	if version >= 2 {
		align := version/7 + 2
		result -= (25*align-10)*align - 55
		if version >= 7 {
			result -= 36
		}
	}
	return result
}

func dataCodewords(version int, ecc byte) int {
	return rawDataModules(version)/8 - eccPerBlock[ecc][version]*blocks[ecc][version]
}

func segmentBits(version, n int) int {
	count := 16
	if version <= 9 {
		count = 8
	}
	return 4 + count + 8*n
}

func fittingVersion(n int, ecc byte) int {
	for version := 1; version <= 40; version++ {
		limit := 0xffff
		if version <= 9 {
			limit = 0xff
		}
		if n <= limit && segmentBits(version, n) <= dataCodewords(version, ecc)*8 {
			return version
		}
	}
	return 0
}

// Encode encodes text as UTF-8 in one byte-mode segment at the smallest
// version that holds it at level L, then raises the level as far as that
// version allows.
func Encode(text string) (Code, error) {
	data := []byte(text)
	version := fittingVersion(len(data), 'L')
	if version == 0 {
		return Code{}, fmt.Errorf("%d bytes do not fit in a QR code", len(data))
	}
	ecc := byte('L')
	for _, stronger := range eccOrder[1:] {
		if segmentBits(version, len(data)) <= dataCodewords(version, stronger)*8 {
			ecc = stronger
		}
	}
	return place(withErrorCorrection(dataStream(data, version, ecc), version, ecc), version, ecc), nil
}

func dataStream(data []byte, version int, ecc byte) []byte {
	capacity := dataCodewords(version, ecc) * 8
	var bits []byte
	push := func(value, length int) {
		for i := length - 1; i >= 0; i-- {
			bits = append(bits, byte((value>>i)&1))
		}
	}
	push(0b0100, 4)
	if version <= 9 {
		push(len(data), 8)
	} else {
		push(len(data), 16)
	}
	for _, b := range data {
		push(int(b), 8)
	}
	push(0, min(4, capacity-len(bits)))
	push(0, (8-len(bits)%8)%8)
	for pad := 0xec; len(bits) < capacity; pad ^= 0xec ^ 0x11 {
		push(pad, 8)
	}
	out := make([]byte, capacity/8)
	for i, b := range bits {
		out[i>>3] |= b << (7 - (i & 7))
	}
	return out
}

func gfMultiply(x, y int) int {
	z := 0
	for i := 7; i >= 0; i-- {
		z = (z << 1) ^ ((z >> 7) * 0x11d)
		z ^= ((y >> i) & 1) * x
	}
	return z
}

func rsDivisor(degree int) []int {
	result := make([]int, degree)
	result[degree-1] = 1
	root := 1
	for i := 0; i < degree; i++ {
		for j := 0; j < degree; j++ {
			result[j] = gfMultiply(result[j], root)
			if j+1 < degree {
				result[j] ^= result[j+1]
			}
		}
		root = gfMultiply(root, 0x02)
	}
	return result
}

func rsRemainder(data []byte, divisor []int) []byte {
	result := make([]int, len(divisor))
	for _, b := range data {
		factor := int(b) ^ result[0]
		copy(result, result[1:])
		result[len(result)-1] = 0
		for i := range result {
			result[i] ^= gfMultiply(divisor[i], factor)
		}
	}
	out := make([]byte, len(result))
	for i, v := range result {
		out[i] = byte(v)
	}
	return out
}

func withErrorCorrection(data []byte, version int, ecc byte) []byte {
	blockCount := blocks[ecc][version]
	eccLength := eccPerBlock[ecc][version]
	raw := rawDataModules(version) / 8
	shortBlocks := blockCount - raw%blockCount
	shortLength := raw / blockCount
	divisor := rsDivisor(eccLength)

	var all [][]byte
	for i, k := 0, 0; i < blockCount; i++ {
		take := shortLength - eccLength
		if i >= shortBlocks {
			take++
		}
		chunk := data[k : k+take]
		k += take
		block := append(append([]byte{}, chunk...), rsRemainder(chunk, divisor)...)
		all = append(all, block)
	}
	var out []byte
	for i := 0; i < shortLength-eccLength+1; i++ {
		for _, block := range all {
			if i < len(block)-eccLength {
				out = append(out, block[i])
			}
		}
	}
	for i := 0; i < eccLength; i++ {
		for _, block := range all {
			out = append(out, block[len(block)-eccLength+i])
		}
	}
	return out
}

func alignmentPositions(version int) []int {
	if version == 1 {
		return nil
	}
	count := version/7 + 2
	size := 17 + 4*version
	step := 26
	if version != 32 {
		step = int(math.Ceil(float64(version*4+4)/float64(count*2-2))) * 2
	}
	result := []int{6}
	for pos := size - 7; len(result) < count; pos -= step {
		result = append(result[:1], append([]int{pos}, result[1:]...)...)
	}
	return result
}

func maskBit(mask, x, y int) bool {
	switch mask {
	case 0:
		return (x+y)%2 == 0
	case 1:
		return y%2 == 0
	case 2:
		return x%3 == 0
	case 3:
		return (x+y)%3 == 0
	case 4:
		return (x/3+y/2)%2 == 0
	case 5:
		return (x*y)%2+(x*y)%3 == 0
	case 6:
		return ((x*y)%2+(x*y)%3)%2 == 0
	default:
		return ((x+y)%2+(x*y)%3)%2 == 0
	}
}

func place(codewords []byte, version int, ecc byte) Code {
	size := 17 + 4*version
	dark := make([]bool, size*size)
	fixed := make([]bool, size*size)
	set := func(x, y int, on bool) {
		dark[y*size+x] = on
		fixed[y*size+x] = true
	}
	for i := 0; i < size; i++ {
		set(6, i, i%2 == 0)
		set(i, 6, i%2 == 0)
	}
	finder := func(cx, cy int) {
		for dy := -4; dy <= 4; dy++ {
			for dx := -4; dx <= 4; dx++ {
				x, y := cx+dx, cy+dy
				if x < 0 || y < 0 || x >= size || y >= size {
					continue
				}
				d := max(abs(dx), abs(dy))
				set(x, y, d != 2 && d != 4)
			}
		}
	}
	finder(3, 3)
	finder(size-4, 3)
	finder(3, size-4)
	aligns := alignmentPositions(version)
	last := len(aligns) - 1
	for i, cx := range aligns {
		for j, cy := range aligns {
			if (i == 0 && j == 0) || (i == 0 && j == last) || (i == last && j == 0) {
				continue
			}
			for dy := -2; dy <= 2; dy++ {
				for dx := -2; dx <= 2; dx++ {
					set(cx+dx, cy+dy, max(abs(dx), abs(dy)) != 1)
				}
			}
		}
	}
	drawFormat(set, size, ecc, 0)
	if version >= 7 {
		rem := version
		for i := 0; i < 12; i++ {
			rem = (rem << 1) ^ ((rem >> 11) * 0x1f25)
		}
		bits := version<<12 | rem
		for i := 0; i < 18; i++ {
			on := (bits>>i)&1 == 1
			a := size - 11 + i%3
			b := i / 3
			set(a, b, on)
			set(b, a, on)
		}
	}

	bit := 0
	total := len(codewords) * 8
	for right := size - 1; right >= 1; right -= 2 {
		if right == 6 {
			right = 5
		}
		upward := (right+1)&2 == 0
		for vert := 0; vert < size; vert++ {
			y := vert
			if upward {
				y = size - 1 - vert
			}
			for j := 0; j < 2; j++ {
				x := right - j
				if fixed[y*size+x] || bit >= total {
					continue
				}
				dark[y*size+x] = (codewords[bit>>3]>>(7-(bit&7)))&1 == 1
				bit++
			}
		}
	}

	best, bestScore := 0, math.MaxInt
	var bestDark []bool
	for mask := 0; mask < 8; mask++ {
		trial := append([]bool{}, dark...)
		for y := 0; y < size; y++ {
			for x := 0; x < size; x++ {
				if !fixed[y*size+x] && maskBit(mask, x, y) {
					trial[y*size+x] = !trial[y*size+x]
				}
			}
		}
		drawFormat(func(x, y int, on bool) { trial[y*size+x] = on }, size, ecc, mask)
		if score := penalty(trial, size); score < bestScore {
			best, bestScore, bestDark = mask, score, trial
		}
	}
	return Code{Version: version, Size: size, ECC: ecc, Mask: best, Dark: bestDark}
}

func drawFormat(set func(x, y int, on bool), size int, ecc byte, mask int) {
	data := formatBits[ecc]<<3 | mask
	rem := data
	for i := 0; i < 10; i++ {
		rem = (rem << 1) ^ ((rem >> 9) * 0x537)
	}
	bits := (data<<10 | rem) ^ 0x5412
	on := func(i int) bool { return (bits>>i)&1 == 1 }
	for i := 0; i <= 5; i++ {
		set(8, i, on(i))
	}
	set(8, 7, on(6))
	set(8, 8, on(7))
	set(7, 8, on(8))
	for i := 9; i < 15; i++ {
		set(14-i, 8, on(i))
	}
	for i := 0; i < 8; i++ {
		set(size-1-i, 8, on(i))
	}
	for i := 8; i < 15; i++ {
		set(8, size-15+i, on(i))
	}
	set(8, size-8, true)
}

func penalty(dark []bool, size int) int {
	score := 0
	at := func(x, y int) bool { return dark[y*size+x] }
	finderLike := []bool{true, false, true, true, true, false, true}
	line := make([]bool, size)
	for pass := 0; pass < 2; pass++ {
		for a := 0; a < size; a++ {
			for b := 0; b < size; b++ {
				if pass == 0 {
					line[b] = at(b, a)
				} else {
					line[b] = at(a, b)
				}
			}
			run := 1
			for b := 1; b <= size; b++ {
				if b < size && line[b] == line[b-1] {
					run++
					continue
				}
				if run >= 5 {
					score += 3 + (run - 5)
				}
				run = 1
			}
			for b := 0; b+7 <= size; b++ {
				match := true
				for k, v := range finderLike {
					if line[b+k] != v {
						match = false
						break
					}
				}
				if !match {
					continue
				}
				before, after := true, true
				for k := 1; k <= 4; k++ {
					if b-k >= 0 && line[b-k] {
						before = false
					}
				}
				for k := 0; k <= 3; k++ {
					if b+7+k < size && line[b+7+k] {
						after = false
					}
				}
				if before || after {
					score += 40
				}
			}
		}
	}
	for y := 0; y+1 < size; y++ {
		for x := 0; x+1 < size; x++ {
			c := at(x, y)
			if c == at(x+1, y) && c == at(x, y+1) && c == at(x+1, y+1) {
				score += 3
			}
		}
	}
	darkCount := 0
	for _, v := range dark {
		if v {
			darkCount++
		}
	}
	total := size * size
	score += (int(math.Ceil(math.Abs(float64(darkCount*20-total*10))/float64(total))) - 1) * 10
	return score
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// Quiet is the light border the standard asks for, in modules.
const Quiet = 4

// Terminal draws the code with half-block characters, two module rows per text
// line, dark modules as the terminal's foreground on its background. Light and
// dark are drawn explicitly (a space is light) so a dark-themed terminal, where
// the foreground is light, inverts the code; the result is still read by a
// phone camera, which decodes either polarity.
func (c Code) Terminal() string {
	n := c.Size + 2*Quiet
	at := func(x, y int) bool {
		x -= Quiet
		y -= Quiet
		if x < 0 || y < 0 || x >= c.Size || y >= c.Size {
			return false
		}
		return c.Dark[y*c.Size+x]
	}
	var b strings.Builder
	for y := 0; y < n; y += 2 {
		for x := 0; x < n; x++ {
			top, bottom := at(x, y), y+1 < n && at(x, y+1)
			switch {
			case top && bottom:
				b.WriteRune('█')
			case top:
				b.WriteRune('▀')
			case bottom:
				b.WriteRune('▄')
			default:
				b.WriteByte(' ')
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}

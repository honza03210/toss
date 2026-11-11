package main

// A small QR code encoder (ISO/IEC 18004): byte mode, error correction level L or M,
// versions 1–10 (up to 271 bytes), which is plenty for a pairing link. It follows the
// structure of Project Nayuki's reference implementation.

import (
	"errors"
	"strings"
)

type qrECC int

const (
	eccL qrECC = iota
	eccM
)

// Per version 1…10 (index 0 unused).
var qrECCPerBlock = [2][11]int{
	{0, 7, 10, 15, 20, 26, 18, 20, 24, 30, 18},
	{0, 10, 16, 26, 18, 24, 16, 18, 22, 22, 26},
}
var qrBlocks = [2][11]int{
	{0, 1, 1, 1, 1, 1, 2, 2, 2, 2, 4},
	{0, 1, 1, 1, 2, 2, 4, 4, 4, 5, 5},
}

// Format bits for each level as the standard numbers them (L=01, M=00).
var qrFormatBits = [2]int{1, 0}

type QRCode struct {
	Size    int
	modules [][]bool
	isFunc  [][]bool
}

// Dark reports whether the module at column x, row y is dark.
func (q *QRCode) Dark(x, y int) bool {
	return x >= 0 && y >= 0 && x < q.Size && y < q.Size && q.modules[y][x]
}

func qrRawDataModules(ver int) int {
	result := (16*ver+128)*ver + 64
	if ver >= 2 {
		numAlign := ver/7 + 2
		result -= (25*numAlign-10)*numAlign - 55
		if ver >= 7 {
			result -= 36
		}
	}
	return result
}

func qrDataCodewords(ver int, ecc qrECC) int {
	return qrRawDataModules(ver)/8 - qrECCPerBlock[ecc][ver]*qrBlocks[ecc][ver]
}

// EncodeQR picks the smallest version that fits `data` at level `ecc`.
func EncodeQR(data []byte, ecc qrECC) (*QRCode, error) {
	for ver := 1; ver <= 10; ver++ {
		countBits := 8
		if ver >= 10 {
			countBits = 16
		}
		capacityBits := qrDataCodewords(ver, ecc) * 8
		if 4+countBits+8*len(data) > capacityBits {
			continue
		}
		// Bit stream: byte mode, length, data, terminator, padding.
		var bits []bool
		appendBits := func(v, n int) {
			for i := n - 1; i >= 0; i-- {
				bits = append(bits, (v>>i)&1 == 1)
			}
		}
		appendBits(0x4, 4)
		appendBits(len(data), countBits)
		for _, b := range data {
			appendBits(int(b), 8)
		}
		terminator := capacityBits - len(bits)
		if terminator > 4 {
			terminator = 4
		}
		appendBits(0, terminator)
		appendBits(0, (8-len(bits)%8)%8)
		for pad := 0xEC; len(bits) < capacityBits; pad ^= 0xEC ^ 0x11 {
			appendBits(pad, 8)
		}
		codewords := make([]byte, len(bits)/8)
		for i, bit := range bits {
			if bit {
				codewords[i>>3] |= 1 << (7 - uint(i&7))
			}
		}
		return qrBuild(ver, ecc, codewords), nil
	}
	return nil, errors.New("data too long for a QR code up to version 10")
}

func qrBuild(ver int, ecc qrECC, data []byte) *QRCode {
	size := ver*4 + 17
	q := &QRCode{Size: size, modules: make([][]bool, size), isFunc: make([][]bool, size)}
	for i := range q.modules {
		q.modules[i] = make([]bool, size)
		q.isFunc[i] = make([]bool, size)
	}
	q.drawFunctionPatterns(ver, ecc)
	all := qrAddECCAndInterleave(ver, ecc, data)
	q.drawCodewords(all)

	// Pick the mask with the lowest penalty, as the standard asks.
	best, bestPenalty := 0, -1
	for mask := 0; mask < 8; mask++ {
		q.applyMask(mask)
		q.drawFormatBits(ecc, mask)
		if p := q.penalty(); bestPenalty < 0 || p < bestPenalty {
			best, bestPenalty = mask, p
		}
		q.applyMask(mask) // XOR again to undo
	}
	q.applyMask(best)
	q.drawFormatBits(ecc, best)
	return q
}

func (q *QRCode) set(x, y int, dark bool) {
	q.modules[y][x] = dark
	q.isFunc[y][x] = true
}

func (q *QRCode) drawFunctionPatterns(ver int, ecc qrECC) {
	for i := 0; i < q.Size; i++ {
		q.set(6, i, i%2 == 0)
		q.set(i, 6, i%2 == 0)
	}
	q.drawFinder(3, 3)
	q.drawFinder(q.Size-4, 3)
	q.drawFinder(3, q.Size-4)
	align := qrAlignmentPositions(ver)
	n := len(align)
	for i := 0; i < n; i++ {
		for j := 0; j < n; j++ {
			if (i == 0 && j == 0) || (i == 0 && j == n-1) || (i == n-1 && j == 0) {
				continue
			}
			q.drawAlignment(align[i], align[j])
		}
	}
	q.drawFormatBits(ecc, 0) // reserves the area; redrawn after masking
	q.drawVersion(ver)
}

func (q *QRCode) drawFinder(x, y int) {
	for dy := -4; dy <= 4; dy++ {
		for dx := -4; dx <= 4; dx++ {
			dist := max(abs(dx), abs(dy))
			xx, yy := x+dx, y+dy
			if xx >= 0 && xx < q.Size && yy >= 0 && yy < q.Size {
				q.set(xx, yy, dist != 2 && dist != 4)
			}
		}
	}
}

func (q *QRCode) drawAlignment(x, y int) {
	for dy := -2; dy <= 2; dy++ {
		for dx := -2; dx <= 2; dx++ {
			q.set(x+dx, y+dy, max(abs(dx), abs(dy)) != 1)
		}
	}
}

func qrAlignmentPositions(ver int) []int {
	if ver == 1 {
		return nil
	}
	numAlign := ver/7 + 2
	step := (ver*4 + numAlign*2 + 1) / (numAlign*2 - 2) * 2
	result := make([]int, numAlign)
	result[0] = 6
	for i, pos := numAlign-1, ver*4+17-7; i >= 1; i, pos = i-1, pos-step {
		result[i] = pos
	}
	return result
}

func (q *QRCode) drawFormatBits(ecc qrECC, mask int) {
	data := qrFormatBits[ecc]<<3 | mask
	rem := data
	for i := 0; i < 10; i++ {
		rem = (rem << 1) ^ ((rem >> 9) * 0x537)
	}
	bits := (data<<10 | rem) ^ 0x5412
	bit := func(i int) bool { return (bits>>i)&1 == 1 }
	for i := 0; i <= 5; i++ {
		q.set(8, i, bit(i))
	}
	q.set(8, 7, bit(6))
	q.set(8, 8, bit(7))
	q.set(7, 8, bit(8))
	for i := 9; i < 15; i++ {
		q.set(14-i, 8, bit(i))
	}
	for i := 0; i < 8; i++ {
		q.set(q.Size-1-i, 8, bit(i))
	}
	for i := 8; i < 15; i++ {
		q.set(8, q.Size-15+i, bit(i))
	}
	q.set(8, q.Size-8, true) // the dark module
}

func (q *QRCode) drawVersion(ver int) {
	if ver < 7 {
		return
	}
	rem := ver
	for i := 0; i < 12; i++ {
		rem = (rem << 1) ^ ((rem >> 11) * 0x1F25)
	}
	bits := ver<<12 | rem
	for i := 0; i < 18; i++ {
		dark := (bits>>i)&1 == 1
		a, b := q.Size-11+i%3, i/3
		q.set(a, b, dark)
		q.set(b, a, dark)
	}
}

func qrAddECCAndInterleave(ver int, ecc qrECC, data []byte) []byte {
	numBlocks := qrBlocks[ecc][ver]
	blockECCLen := qrECCPerBlock[ecc][ver]
	rawCodewords := qrRawDataModules(ver) / 8
	numShort := numBlocks - rawCodewords%numBlocks
	shortLen := rawCodewords / numBlocks

	divisor := rsDivisor(blockECCLen)
	blocks := make([][]byte, numBlocks)
	k := 0
	for i := 0; i < numBlocks; i++ {
		n := shortLen - blockECCLen
		if i >= numShort {
			n++
		}
		dat := append([]byte(nil), data[k:k+n]...)
		k += n
		eccBytes := rsRemainder(dat, divisor)
		if i < numShort {
			dat = append(dat, 0) // placeholder so all blocks have equal length
		}
		blocks[i] = append(dat, eccBytes...)
	}
	var result []byte
	for i := range blocks[0] {
		for j, block := range blocks {
			if i != shortLen-blockECCLen || j >= numShort {
				result = append(result, block[i])
			}
		}
	}
	return result
}

func gfMul(x, y byte) byte {
	z := 0
	for i := 7; i >= 0; i-- {
		z = (z << 1) ^ ((z >> 7) * 0x11D)
		z ^= int((y>>uint(i))&1) * int(x)
	}
	return byte(z)
}

func rsDivisor(degree int) []byte {
	result := make([]byte, degree)
	result[degree-1] = 1
	root := byte(1)
	for i := 0; i < degree; i++ {
		for j := range result {
			result[j] = gfMul(result[j], root)
			if j+1 < len(result) {
				result[j] ^= result[j+1]
			}
		}
		root = gfMul(root, 0x02)
	}
	return result
}

func rsRemainder(data, divisor []byte) []byte {
	result := make([]byte, len(divisor))
	for _, b := range data {
		factor := b ^ result[0]
		copy(result, result[1:])
		result[len(result)-1] = 0
		for i, d := range divisor {
			result[i] ^= gfMul(d, factor)
		}
	}
	return result
}

func (q *QRCode) drawCodewords(data []byte) {
	i := 0
	for right := q.Size - 1; right >= 1; right -= 2 {
		if right == 6 {
			right = 5
		}
		for vert := 0; vert < q.Size; vert++ {
			for j := 0; j < 2; j++ {
				x := right - j
				upward := (right+1)&2 == 0
				y := vert
				if upward {
					y = q.Size - 1 - vert
				}
				if !q.isFunc[y][x] && i < len(data)*8 {
					q.modules[y][x] = (data[i>>3]>>(7-uint(i&7)))&1 == 1
					i++
				}
			}
		}
	}
}

func (q *QRCode) applyMask(mask int) {
	for y := 0; y < q.Size; y++ {
		for x := 0; x < q.Size; x++ {
			var invert bool
			switch mask {
			case 0:
				invert = (x+y)%2 == 0
			case 1:
				invert = y%2 == 0
			case 2:
				invert = x%3 == 0
			case 3:
				invert = (x+y)%3 == 0
			case 4:
				invert = (x/3+y/2)%2 == 0
			case 5:
				invert = x*y%2+x*y%3 == 0
			case 6:
				invert = (x*y%2+x*y%3)%2 == 0
			case 7:
				invert = ((x+y)%2+x*y%3)%2 == 0
			}
			if invert && !q.isFunc[y][x] {
				q.modules[y][x] = !q.modules[y][x]
			}
		}
	}
}

// penalty scores runs, 2×2 blocks, finder-like patterns and dark/light balance.
func (q *QRCode) penalty() int {
	score := 0
	line := func(get func(i int) bool) {
		run, prev := 0, false
		var pattern []bool
		for i := 0; i < q.Size; i++ {
			c := get(i)
			if i > 0 && c == prev {
				run++
			} else {
				if run >= 5 {
					score += 3 + run - 5
				}
				run = 1
			}
			prev = c
			pattern = append(pattern, c)
		}
		if run >= 5 {
			score += 3 + run - 5
		}
		// 1:1:3:1:1 finder-like runs with 4 light modules on one side.
		s := make([]byte, 0, len(pattern)+8)
		s = append(s, "0000"...)
		for _, c := range pattern {
			if c {
				s = append(s, '1')
			} else {
				s = append(s, '0')
			}
		}
		s = append(s, "0000"...)
		str := string(s)
		score += 40 * (strings.Count(str, "00001011101") + strings.Count(str, "10111010000"))
	}
	for y := 0; y < q.Size; y++ {
		line(func(i int) bool { return q.modules[y][i] })
	}
	for x := 0; x < q.Size; x++ {
		line(func(i int) bool { return q.modules[i][x] })
	}
	dark := 0
	for y := 0; y < q.Size; y++ {
		for x := 0; x < q.Size; x++ {
			if q.modules[y][x] {
				dark++
			}
			if x+1 < q.Size && y+1 < q.Size {
				c := q.modules[y][x]
				if c == q.modules[y][x+1] && c == q.modules[y+1][x] && c == q.modules[y+1][x+1] {
					score += 3
				}
			}
		}
	}
	total := q.Size * q.Size
	k := (abs(dark*20-total*10)+total-1)/total - 1
	if k > 0 {
		score += k * 10
	}
	return score
}

// Terminal renders the code with Unicode upper half blocks, two modules per character:
// the foreground colour is the top module, the background the bottom one. Colours are set
// explicitly (black on white), so it scans the same on dark and light terminal themes.
// It includes the 4-module quiet zone scanners need.
func (q *QRCode) Terminal() string {
	const quiet = 4
	color := func(dark bool, fg bool) string {
		switch {
		case fg && dark:
			return "30"
		case fg:
			return "97"
		case dark:
			return "40"
		default:
			return "107"
		}
	}
	var b strings.Builder
	for y := -quiet; y < q.Size+quiet; y += 2 {
		for x := -quiet; x < q.Size+quiet; x++ {
			b.WriteString("\x1b[" + color(q.Dark(x, y), true) + ";" + color(q.Dark(x, y+1), false) + "m▀")
		}
		b.WriteString("\x1b[0m\n")
	}
	return b.String()
}

// ASCII renders the code with two characters per module, for logs or terminals without
// colour ("##" is dark).
func (q *QRCode) ASCII() string {
	var b strings.Builder
	for y := -2; y < q.Size+2; y++ {
		for x := -2; x < q.Size+2; x++ {
			if q.Dark(x, y) {
				b.WriteString("##")
			} else {
				b.WriteString("  ")
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

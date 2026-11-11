package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestQRVersionsAndFormat(t *testing.T) {
	for _, tc := range []struct {
		n    int
		ecc  qrECC
		size int
	}{
		{17, eccL, 21}, {18, eccL, 25}, {154, eccL, 45}, {155, eccL, 49}, {271, eccL, 57}, {213, eccM, 57},
	} {
		q, err := EncodeQR([]byte(strings.Repeat("a", tc.n)), tc.ecc)
		if err != nil {
			t.Fatalf("%d bytes: %v", tc.n, err)
		}
		if q.Size != tc.size {
			t.Errorf("%d bytes at %d: size %d, want %d", tc.n, tc.ecc, q.Size, tc.size)
		}
	}
	if _, err := EncodeQR([]byte(strings.Repeat("a", 272)), eccL); err == nil {
		t.Error("272 bytes should not fit")
	}
}

func TestQRFinderAndTiming(t *testing.T) {
	q, _ := EncodeQR([]byte("toss://pair?v=1"), eccL)
	// Finder pattern corners are dark, the separator ring light.
	for _, p := range [][2]int{{0, 0}, {6, 0}, {0, 6}, {q.Size - 1, 0}, {0, q.Size - 1}} {
		if !q.Dark(p[0], p[1]) {
			t.Errorf("finder module %v should be dark", p)
		}
	}
	if q.Dark(7, 0) || q.Dark(0, 7) {
		t.Error("separator should be light")
	}
	for i := 8; i < q.Size-8; i++ {
		if q.Dark(i, 6) != (i%2 == 0) || q.Dark(6, i) != (i%2 == 0) {
			t.Fatalf("timing pattern wrong at %d", i)
		}
	}
	if !q.Dark(8, q.Size-8) {
		t.Error("dark module missing")
	}
}

func TestReedSolomonKnownVector(t *testing.T) {
	// "HELLO WORLD" 1-M example from the QR specification (Thonky tutorial):
	// data codewords and the 10 expected EC codewords.
	data := []byte{32, 91, 11, 120, 209, 114, 220, 77, 67, 64, 236, 17, 236, 17, 236, 17}
	want := []byte{196, 35, 39, 119, 235, 215, 231, 226, 93, 23}
	got := rsRemainder(data, rsDivisor(10))
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("EC codewords %v, want %v", got, want)
	}
}

// With TOSS_QR_DIR set, writes PNGs of many codes for Scripts/check_qr.py to decode.
func TestQRWritePNGs(t *testing.T) {
	dir := os.Getenv("TOSS_QR_DIR")
	if dir == "" {
		t.Skip("set TOSS_QR_DIR to write PNGs")
	}
	var manifest []string
	for _, ecc := range []qrECC{eccL, eccM} {
		for _, n := range []int{5, 17, 30, 50, 75, 100, 130, 150, 170, 190, 210, 230, 260} {
			payload := PairingLink("honza-t490 ěš", []string{"honza-t490.local", "192.168.0.131"}, 47800, randomToken(32), randomToken(16))
			for len(payload) < n {
				payload += "x"
			}
			payload = payload[:n]
			q, err := EncodeQR([]byte(payload), ecc)
			if err != nil {
				continue
			}
			name := fmt.Sprintf("qr-%d-%d.png", ecc, n)
			writePNG(t, q, filepath.Join(dir, name))
			os.WriteFile(filepath.Join(dir, name+".txt"), []byte(payload), 0o644)
			manifest = append(manifest, name)
		}
	}
	os.WriteFile(filepath.Join(dir, "manifest.txt"), []byte(strings.Join(manifest, "\n")), 0o644)
}

func writePNG(t *testing.T, q *QRCode, path string) {
	const scale, quiet = 8, 4
	n := (q.Size + 2*quiet) * scale
	img := image.NewGray(image.Rect(0, 0, n, n))
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			c := color.Gray{255}
			if q.Dark(x/scale-quiet, y/scale-quiet) {
				c = color.Gray{0}
			}
			img.SetGray(x, y, c)
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

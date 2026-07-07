package assets

import (
	"embed"
	"fmt"
	"strings"
)

//go:embed files
var fsys embed.FS

// ROM reassembles the cart image from the named asset tree. The recompiled
// code addresses data by absolute ROM address, so every region must land back
// at its original offset; the result is byte-identical to the source ROM.
func ROM() []byte {
	img := make([]byte, 0, 16*1024*1024)
	for _, r := range regions {
		b, err := fsys.ReadFile(r.path)
		if err != nil {
			panic(err)
		}
		if r.pal {
			b = decodePal(b)
		}
		if len(b) != r.size {
			panic(fmt.Sprintf("%s: size %d, want %d", r.path, len(b), r.size))
		}
		img = append(img, b...)
	}
	return img
}

func decodePal(text []byte) []byte {
	lines := strings.Split(strings.ReplaceAll(string(text), "\r\n", "\n"), "\n")
	out := make([]byte, 0, (len(lines)-3)*2)
	for _, ln := range lines[3:] {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		var r, g, bl int
		fmt.Sscanf(ln, "%d %d %d", &r, &g, &bl)
		c := uint16(r>>3) | uint16(g>>3)<<5 | uint16(bl>>3)<<10
		out = append(out, byte(c), byte(c>>8))
	}
	return out
}

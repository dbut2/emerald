// Script commands: "wait N", "hold BTNS N", "tap BTNS", "shot NAME"; BTNS is
// '+'-joined from: a b select start right left up down r l.
package main

import (
	"bufio"
	"fmt"
	"image/png"
	"os"
	"strconv"
	"strings"

	"dbut.dev/sapphire/gba"

	"dbut.dev/emerald/internal/core"
)

var keyBits = map[string]uint16{
	"a": 1 << 0, "b": 1 << 1, "select": 1 << 2, "start": 1 << 3,
	"right": 1 << 4, "left": 1 << 5, "up": 1 << 6, "down": 1 << 7,
	"r": 1 << 8, "l": 1 << 9,
}

func le16(b []byte, off int) uint16 { return uint16(b[off]) | uint16(b[off+1])<<8 }

func keysFor(spec string) uint16 {
	var mask uint16
	if spec == "" || spec == "none" {
		return 0x03FF
	}
	for _, b := range strings.Split(spec, "+") {
		bit, ok := keyBits[b]
		if !ok {
			fmt.Fprintf(os.Stderr, "unknown button %q\n", b)
			os.Exit(2)
		}
		mask |= bit
	}
	return 0x03FF &^ mask
}

func main() {
	emu := gba.NewEmu(make([]byte, 32*1024*1024))
	c := core.New(emu)

	var lines []string
	if len(os.Args) > 1 {
		data, err := os.ReadFile(os.Args[1])
		if err != nil {
			panic(err)
		}
		lines = strings.Split(string(data), "\n")
	} else {
		sc := bufio.NewScanner(os.Stdin)
		for sc.Scan() {
			lines = append(lines, sc.Text())
		}
	}

	frame := 0
	adv := func(keys uint16, n int) {
		c.StepN(keys, n)
		frame += n
	}
	shot := func(name string) {
		c.Render()
		f, err := os.Create("/tmp/emerald_" + name + ".png")
		if err != nil {
			panic(err)
		}
		defer f.Close()
		if err := png.Encode(f, emu.LCD.Front()); err != nil {
			panic(err)
		}
		fmt.Fprintf(os.Stderr, "[f%d] wrote /tmp/emerald_%s.png\n", frame, name)
	}

	for _, ln := range lines {
		ln = strings.TrimSpace(ln)
		if ln == "" || strings.HasPrefix(ln, "#") {
			continue
		}
		f := strings.Fields(ln)
		switch f[0] {
		case "wait":
			n, _ := strconv.Atoi(f[1])
			adv(0x03FF, n)
		case "hold":
			n, _ := strconv.Atoi(f[2])
			adv(keysFor(f[1]), n)
		case "tap":
			adv(keysFor(f[1]), 6)
			adv(0x03FF, 6)
		case "shot":
			shot(f[1])
		case "dump":
			c.Render()
			r := func(reg gba.IORegister[uint16]) uint16 { return gba.ReadIORegister16(emu.Memory, reg) }
			fmt.Fprintf(os.Stderr, "[f%d] DISPCNT=%04x BG0=%04x BG1=%04x BG2=%04x BG3=%04x BLDCNT=%04x WININ=%04x WINOUT=%04x WIN0H=%04x WIN0V=%04x WIN1H=%04x WIN1V=%04x\n",
				frame, r(gba.DISPCNT), r(gba.BG0CNT), r(gba.BG1CNT), r(gba.BG2CNT), r(gba.BG3CNT), r(gba.BLDCNT), r(gba.WININ), r(gba.WINOUT), r(gba.WIN0H), r(gba.WIN0V), r(gba.WIN1H), r(gba.WIN1V))
			pal := emu.Memory.ReadMemoryBlock(gba.Palette)
			nzBG, nzOBJ := 0, 0
			for i := 0; i < 256; i++ {
				c := uint16(pal[i*2]) | uint16(pal[i*2+1])<<8
				if c != 0 {
					nzBG++
				}
				c2 := uint16(pal[512+i*2]) | uint16(pal[512+i*2+1])<<8
				if c2 != 0 {
					nzOBJ++
				}
			}
			vram := emu.Memory.ReadMemoryBlock(gba.VRAM)
			nz := func(lo, hi int) int {
				c := 0
				for i := lo; i < hi && i < len(vram); i++ {
					if vram[i] != 0 {
						c++
					}
				}
				return c
			}
			fmt.Fprintf(os.Stderr, "  BGpal nz=%d/256 OBJpal nz=%d/256  VRAM BGchar[0:4000]=%d BGmid[4000:e000]=%d BGscreen[e000:10000]=%d OBJ[10000:18000]=%d\n",
				nzBG, nzOBJ, nz(0, 0x4000), nz(0x4000, 0xe000), nz(0xe000, 0x10000), nz(0x10000, 0x18000))
			fmt.Fprintf(os.Stderr, "  BGpal0..7=%04x %04x %04x %04x %04x %04x %04x %04x\n",
				le16(pal, 0), le16(pal, 2), le16(pal, 4), le16(pal, 6), le16(pal, 8), le16(pal, 10), le16(pal, 12), le16(pal, 14))
		default:
			fmt.Fprintf(os.Stderr, "unknown command %q\n", ln)
			os.Exit(2)
		}
	}
	fmt.Fprintf(os.Stderr, "done at frame %d\n", frame)
}

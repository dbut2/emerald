package main

import (
	"image/png"
	"os"

	"dbut.dev/sapphire/gba"

	"github.com/dbut2/emerald/internal/core"
)

func main() {
	emu := gba.NewEmu(make([]byte, 32*1024*1024))
	c := core.New(emu)
	for i := 0; i < 6500; i++ {
		c.Frame(0x03FF)
	}
	f, err := os.Create("/tmp/emerald_native.png")
	if err != nil {
		panic(err)
	}
	defer f.Close()
	if err := png.Encode(f, emu.LCD.Front()); err != nil {
		panic(err)
	}
	os.Stderr.WriteString("wrote /tmp/emerald_native.png (rendered by Sapphire's PPU)\n")
}

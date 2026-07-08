package main

import (
	"time"

	"dbut.dev/sapphire/gba"

	"github.com/dbut2/emerald/internal/core"
)

func main() {
	emu := gba.NewEmu(make([]byte, 32*1024*1024))
	c := core.New(emu)

	RunBoot("Emerald (native cgo)", emu, func() {
		ticker := time.NewTicker(16739 * time.Microsecond)
		defer ticker.Stop()
		for range ticker.C {
			c.Frame(gba.ReadIORegister(emu.Memory, gba.KEYINPUT))
		}
	})
}

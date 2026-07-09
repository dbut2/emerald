package main

import (
	"flag"
	"time"

	"dbut.dev/sapphire/gba"

	"dbut.dev/emerald/internal/core"
	"dbut.dev/emerald/internal/crashreport"
)

const frameDur = 16739 * time.Microsecond

func main() {
	crashreport.Guard()

	turbo := flag.Bool("turbo", false, "start with fast-forward enabled")
	batch := flag.Int("turbo-batch", 1, "frames per bridge crossing while fast-forwarding")
	flag.Parse()

	emu := gba.NewEmu(make([]byte, 32*1024*1024))
	emu.FastForward = *turbo
	c := core.New(emu)

	RunBoot("Pokémon Emerald", emu, func() {
		ticker := time.NewTicker(frameDur)
		defer ticker.Stop()
		lastDraw := time.Now()
		for {
			keys := gba.ReadIORegister(emu.Memory, gba.KEYINPUT)
			if emu.FastForward {
				c.StepN(keys, *batch)
				if time.Since(lastDraw) >= frameDur {
					lastDraw = time.Now()
					for i := 1; i < *batch; i++ {
						emu.LCD.CountFrame()
					}
					c.Render()
				} else {
					for i := 0; i < *batch; i++ {
						emu.LCD.CountFrame()
					}
				}
				continue
			}
			<-ticker.C
			c.Step(keys)
			c.Render()
			lastDraw = time.Now()
		}
	})
}

// Package core drives the native pokeemerald cgo core and renders each frame
// through Sapphire's PPU: it advances the game one frame, mirrors the core's
// VRAM/OAM/palette/IO into Sapphire's Memory, then runs Sapphire's LCD.
package core

/*
#cgo LDFLAGS: ${SRCDIR}/../../port/build/libpe.a -lm
extern void pe_init(void);
extern void pe_run_frame(unsigned short keys);
extern void pe_run_frames(unsigned short keys, int n);
extern unsigned char *pe_base(void);
*/
import "C"

import (
	"unsafe"

	"dbut.dev/sapphire/gba"
)

type Core struct {
	emu                              *gba.Emulator
	io, pal, vram, oam               []byte
	ioSrc, palSrc, vramSrc, oamSrc   []byte
}

func New(emu *gba.Emulator) *Core {
	C.pe_init()
	base := unsafe.Pointer(C.pe_base())
	region := func(off, n int) []byte { return unsafe.Slice((*byte)(unsafe.Add(base, off)), n) }
	return &Core{
		emu:     emu,
		io:      emu.Memory.ReadMemoryBlock(gba.IOR),
		pal:     emu.Memory.ReadMemoryBlock(gba.Palette),
		vram:    emu.Memory.ReadMemoryBlock(gba.VRAM),
		oam:     emu.Memory.ReadMemoryBlock(gba.OAM),
		ioSrc:   region(0x2000000, 0x400),
		palSrc:  region(0x3000000, 0x400),
		vramSrc: region(0x4000000, 0x18000),
		oamSrc:  region(0x5000000, 0x400),
	}
}

func (c *Core) Frame(keys uint16) {
	c.Step(keys)
	c.Render()
}

// Step advances the game one frame. Skipping it yields slow-motion, not
// fast-forward: only Render may be skipped during frame-skip.
func (c *Core) Step(keys uint16) {
	C.pe_run_frame(C.ushort(keys))
}

// StepN advances n frames per bridge crossing, amortising the Go/C rendezvous.
func (c *Core) StepN(keys uint16, n int) {
	C.pe_run_frames(C.ushort(keys), C.int(n))
}

func (c *Core) Render() {
	copy(c.io, c.ioSrc)
	copy(c.pal, c.palSrc)
	copy(c.vram, c.vramSrc)
	copy(c.oam, c.oamSrc)

	c.emu.LCD.LatchAffineRefs()
	blank := gba.ReadBits(gba.ReadIORegister16(c.emu.Memory, gba.DISPCNT), 7, 1)
	for line := uint16(0); line < 160; line++ {
		c.emu.LCD.DrawLine(line, blank)
		c.emu.LCD.IncrementAffineRefs()
	}
	c.emu.LCD.DrawFrame()
}

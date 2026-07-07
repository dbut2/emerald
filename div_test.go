package emerald

import (
	"testing"

	"dbut.dev/emerald/assets"
	"dbut.dev/sapphire/gba"
)

func TestNativeDivsi3(t *testing.T) {
	Install()
	emu := gba.NewEmu(assets.ROM())
	emu.PreBoot()

	c := emu.CPU
	nums := []int32{0x700, 1, 7, -7, 0x1000000, 42, 0x0400C4, 274102272, -274102272, 0x7FFFFFFF}
	dens := []int32{1, 2, 3, 16, 0x60, -3, 1000, 5734, 0x10000, 0x2000000}
	for _, n := range nums {
		for _, d := range dens {
			c.R[0] = uint32(n)
			c.R[1] = uint32(d)
			c.R[14] = 0x08001199
			c.CPSR = gba.SYS
			c.NCallArm(0x082E4DB4)
			if int32(c.R[0]) != n/d {
				t.Errorf("%d/%d = %d, want %d", n, d, int32(c.R[0]), n/d)
			}
			if c.R[14] != 0x08001199 {
				t.Fatalf("LR clobbered doing %d/%d: %08X", n, d, c.R[14])
			}
		}
	}
}

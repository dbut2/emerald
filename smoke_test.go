package emerald

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"runtime/debug"
	"testing"
	"time"

	"dbut.dev/emerald/assets"
	"dbut.dev/sapphire/gba"
)

func TestEmeraldNativeSmoke(t *testing.T) {
	outDir := os.Getenv("EMERALD_SMOKE_OUT")
	if outDir == "" {
		t.Skip("EMERALD_SMOKE_OUT not set")
	}

	Install()
	emu := gba.NewEmu(assets.ROM())
	emu.LCD.ShowFPS = false
	emu.PreBoot()

	const lastFrame = 2600
	checkpoints := map[int]bool{200: true, 700: true, 1500: true, lastFrame: true}

	type shot struct {
		frame int
		img   *image.RGBA
	}
	shots := make(chan shot, len(checkpoints))
	errc := make(chan error, 1)

	frames := 0
	start := time.Now()
	emu.CPU.PaceFrame = func() {
		frames++
		if checkpoints[frames] {
			src := emu.LCD.Front()
			cp := image.NewRGBA(src.Bounds())
			copy(cp.Pix, src.Pix)
			shots <- shot{frames, cp}
		}
		if frames == lastFrame {
			t.Logf("%d frames in %v (%.0f fps)", frames, time.Since(start), float64(frames)/time.Since(start).Seconds())
			select {}
		}
	}

	go func() {
		defer func() {
			if r := recover(); r != nil {
				stack := string(debug.Stack())
				if len(stack) > 4000 {
					stack = stack[:4000]
				}
				errc <- fmt.Errorf("panic: %v\n%s", r, stack)
			}
		}()
		Entry(emu.CPU)
		errc <- fmt.Errorf("entry returned unexpectedly, U=%08X R=%08X", emu.CPU.U, emu.CPU.R)
	}()

	got := 0
	for got < len(checkpoints) {
		select {
		case s := <-shots:
			f, err := os.Create(fmt.Sprintf("%s/native_%04d.png", outDir, s.frame))
			if err != nil {
				t.Fatal(err)
			}
			if err := png.Encode(f, s.img); err != nil {
				t.Fatal(err)
			}
			f.Close()
			got++
		case err := <-errc:
			t.Fatal(err)
		case <-time.After(120 * time.Second):
			t.Fatalf("timeout waiting for frames (got %d checkpoints)", got)
		}
	}
}

package main

// Copied from dbut.dev/sapphire/desktop and adapted: the upstream Start() runs
// emu.Boot() (its interpreter) instead of the boot func, so it can't drive an
// external core. This copy runs w.boot() as RunBoot intends. Kept in-repo per
// "don't touch Sapphire".

import (
	"image"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"

	"dbut.dev/sapphire/gba"
)

func RunBoot(title string, emu *gba.Emulator, boot func()) {
	a := app.New()
	win := window{
		emu:    emu,
		boot:   boot,
		window: a.NewWindow(title),
	}
	win.Start()
}

type window struct {
	emu    *gba.Emulator
	boot   func()
	window fyne.Window

	mu      sync.Mutex
	pressed map[fyne.KeyName]bool
}

var keyMap = map[fyne.KeyName]uint16{
	fyne.KeyZ:         1 << 0, // A
	fyne.KeyX:         1 << 1, // B
	fyne.KeyBackspace: 1 << 2, // Select
	fyne.KeyReturn:    1 << 3, // Start
	fyne.KeyRight:     1 << 4, // Right
	fyne.KeyLeft:      1 << 5, // Left
	fyne.KeyUp:        1 << 6, // Up
	fyne.KeyDown:      1 << 7, // Down
	fyne.KeyS:         1 << 8, // R
	fyne.KeyA:         1 << 9, // L
}

func (w *window) updateKeyInput() {
	w.mu.Lock()
	var mask uint16
	for key := range w.pressed {
		mask |= keyMap[key]
	}
	w.mu.Unlock()
	gba.SetIORegister(w.emu.Memory, gba.KEYINPUT, 0x03FF&^mask)
}

func (w *window) Start() {
	w.pressed = make(map[fyne.KeyName]bool)

	cimg := canvas.NewImageFromImage(w.emu.LCD.Front())
	cimg.ScaleMode = canvas.ImageScalePixels

	// fyne reads asynchronously on its own thread while the game thread keeps
	// overwriting Front(); snapshot per frame so a mid-copy read can't tear text.
	w.emu.LCD.SetDraw(func() {
		w.updateKeyInput()
		src := w.emu.LCD.Front()
		snap := image.NewRGBA(src.Bounds())
		copy(snap.Pix, src.Pix)
		fyne.Do(func() {
			cimg.Image = snap
			cimg.Refresh()
		})
	})

	w.window.SetContent(cimg)
	w.window.Resize(fyne.NewSize(480, 320))

	if dc, ok := w.window.Canvas().(desktop.Canvas); ok {
		dc.SetOnKeyDown(func(event *fyne.KeyEvent) {
			if event.Name == fyne.KeySpace {
				w.emu.FastForward = true
				return
			}
			if _, mapped := keyMap[event.Name]; mapped {
				w.mu.Lock()
				w.pressed[event.Name] = true
				w.mu.Unlock()
			}
		})
		dc.SetOnKeyUp(func(event *fyne.KeyEvent) {
			if event.Name == fyne.KeySpace {
				w.emu.FastForward = false
				return
			}
			w.mu.Lock()
			delete(w.pressed, event.Name)
			w.mu.Unlock()
		})
	}

	go w.boot()

	w.window.ShowAndRun()
}

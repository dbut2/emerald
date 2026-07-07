package main

import (
	"os"
	"path/filepath"

	"dbut.dev/emerald"
	"dbut.dev/emerald/assets"
	"dbut.dev/sapphire/desktop"
	"dbut.dev/sapphire/gba"
)

func main() {
	savePath := deriveSavePath()

	emerald.Install()
	emu := gba.NewEmu(assets.ROM())

	if saved, err := os.ReadFile(savePath); err == nil {
		emu.Flash.LoadData(saved)
	}
	emu.Flash.OnSave(func(saved []byte) {
		_ = os.WriteFile(savePath, saved, 0644)
	})

	desktop.RunBoot("Emerald (native Go)", emu, func() {
		emu.BootNative(emerald.Entry)
	})
}

func deriveSavePath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "emerald.sav"
	}
	dir = filepath.Join(dir, "sapphire")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "emerald.sav"
	}
	return filepath.Join(dir, "emerald.sav")
}

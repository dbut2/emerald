package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
)

func main() {
	elfPath := flag.String("elf", "", "path to pokeemerald_modern.elf")
	outDir := flag.String("out", "", "output directory for generated Go (empty: analyze only)")
	flag.Parse()

	im, err := loadImage(*elfPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	fmt.Printf("entry %08X, %d functions\n", im.Entry, len(im.Funcs))

	a := analyze(im)

	keys := make([]string, 0, len(a.Stats))
	for k := range a.Stats {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("%-24s %d\n", k, a.Stats[k])
	}

	for k, v := range a.Samples {
		for _, s := range v {
			fmt.Printf("SAMPLE %s: %s\n", k, s)
		}
	}
	trapCount := 0
	for _, fc := range a.Fns {
		for _, addr := range fc.Order {
			if fc.Insts[addr].Kind == KTrap && trapCount < 20 {
				chain := ""
				for p, i := addr, 0; p > 1 && i < 10; i++ {
					chain += fmt.Sprintf(" <- %08X", p)
					p = fc.Prov[p]
				}
				fmt.Printf("TRAP %08X in %s raw=%08X mode=%d entries=%v chain:%s\n", addr, fc.Fn.Name, fc.Insts[addr].Raw, fc.Insts[addr].Mode, fc.Entries, chain)
				trapCount++
			}
		}
	}

	for _, fc := range a.Fns {
		for _, e := range fc.Entries {
			fmt.Printf("ENTRY %s+0x%X: %08X mode=%d\n", fc.Fn.Name, e.Addr-fc.Fn.Addr, e.Addr, e.Mode)
		}
	}

	shown := 0
	for _, fc := range a.Fns {
		for _, t := range fc.Traps {
			if shown < 25 {
				fmt.Println("FNTRAP:", t)
				shown++
			}
		}
	}

	armFns := 0
	for _, fc := range a.Fns {
		if !fc.Fn.Thumb {
			armFns++
			if armFns <= 25 {
				fmt.Printf("ARM fn: %s @%08X size=%d\n", fc.Fn.Name, fc.Fn.Addr, fc.Fn.Size)
			}
		}
	}
	fmt.Printf("ARM functions: %d\n", armFns)

	if *outDir != "" {
		if err := emit(a, *outDir); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
}

package main

import (
	"fmt"
	"sort"
)

// One package per source object file. Cross-package calls go through the
// native registry (callExpr), never a direct import, so the game's pervasively
// cyclic call graph can't produce a Go import cycle.
func buildGroups(a *Analysis, ranges []objRange) {
	a.File = map[uint32]string{}
	a.Pkg = map[uint32]string{}
	a.PkgIdent = map[string]string{}

	identOwner := map[string]string{}
	for _, fc := range a.Fns {
		g := groupOf(ranges, fc.Fn.Addr)
		if g == "" {
			g = "unattributed"
		}
		a.File[fc.Fn.Addr] = g
		a.Pkg[fc.Fn.Addr] = g
		if _, ok := a.PkgIdent[g]; !ok {
			id := pkgIdent(g)
			for owner, ok := identOwner[id]; ok && owner != g; owner, ok = identOwner[id] {
				id += "_"
			}
			identOwner[id] = g
			a.PkgIdent[g] = id
		}
	}
}

func reportGroups(a *Analysis) {
	counts := map[string]int{}
	for _, fc := range a.Fns {
		counts[a.Pkg[fc.Fn.Addr]]++
	}
	type gc struct {
		name string
		n    int
	}
	var gs []gc
	for g, n := range counts {
		gs = append(gs, gc{g, n})
	}
	sort.Slice(gs, func(i, j int) bool { return gs[i].n > gs[j].n })
	fmt.Printf("packages: %d\n", len(gs))
	for i, g := range gs {
		if i < 15 {
			fmt.Printf("  %-32s fns=%d\n", g.name, g.n)
		}
	}
}

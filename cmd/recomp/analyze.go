package main

import (
	"fmt"
	"sort"
	"strings"
)

type Mode uint8

const (
	ModeThumb Mode = iota
	ModeArm
)

type InstKind uint8

const (
	KGeneric InstKind = iota
	KCondBranch
	KBranch
	KBranchLink
	KBX
	KBXPC
	KMovPC
	KPopPC
	KSWI
	KLdrPCStatic
	KLdmPC
	KAluPC
	KTrap
	KFarJump
)

type Inst struct {
	Addr    uint32
	Mode    Mode
	TMode   Mode
	Raw     uint32
	Size    uint32
	Kind    InstKind
	Target  uint32
	Reg     uint32
	Cond    uint32
	ReadsPC bool
}

type Entry struct {
	Addr     uint32
	Mode     Mode
	FromPool bool
}

type FnCode struct {
	Fn        Fn
	End       uint32
	Insts     map[uint32]*Inst
	Order     []uint32
	Labels    map[uint32]bool
	JTTargets []uint32
	Entries   []Entry
	Traps     []string
	Prov      map[uint32]uint32
}

func (fc *FnCode) hasEntry(addr uint32) bool {
	for _, e := range fc.Entries {
		if e.Addr == addr {
			return true
		}
	}
	return false
}

type Analysis struct {
	Im       *Image
	Fns      []*FnCode
	ByAddr   map[uint32]*FnCode
	Stats    map[string]int
	Samples  map[string][]string
	Rejected map[uint32]bool
	Names    map[uint32]string

	Module   string
	File     map[uint32]string
	Pkg      map[uint32]string
	PkgIdent map[string]string

	curPkg string
}

func analyze(im *Image) *Analysis {
	a := &Analysis{
		ByAddr:   map[uint32]*FnCode{},
		Im:       im,
		Stats:    map[string]int{},
		Samples:  map[string][]string{},
		Rejected: map[uint32]bool{},
	}

	for _, fn := range im.Funcs {
		fc := &FnCode{Fn: fn, End: fn.Addr + fn.Size}
		if fn.Size == 0 {
			fc.End = fn.Addr + 4
		}
		walk(im, fc, a.Stats)
		a.Fns = append(a.Fns, fc)
		a.ByAddr[fn.Addr] = fc
	}

	if a.ByAddr[im.Entry] == nil {
		walk(im, a.synthesize(im.Entry, ModeArm), a.Stats)
	}

	for round := 0; ; round++ {
		if round > 20 {
			panic("multi-entry fixpoint did not converge")
		}
		dirty := map[*FnCode]bool{}
		type synthReq struct {
			addr uint32
			mode Mode
		}
		var synths []synthReq
		for _, fc := range a.Fns {
			resolve := func(in *Inst, tgt uint32, isCall bool) {
				if a.ByAddr[tgt] != nil {
					if !isCall {
						a.Stats["tailcall"]++
					}
					return
				}
				target := a.containing(tgt)
				if target == nil {
					if !a.Im.InCode(tgt) {
						a.Stats["trap.branch-outside-code"]++
						return
					}
					synths = append(synths, synthReq{tgt, in.TMode})
					return
				}
				if isCall {
					a.Stats["bl-mid-function"]++
				}
				if !target.hasEntry(tgt) {
					target.Entries = append(target.Entries, Entry{tgt, in.TMode, false})
					dirty[target] = true
					if len(a.Samples["entry-add"]) < 60 {
						a.Samples["entry-add"] = append(a.Samples["entry-add"], fmt.Sprintf("%08X (%s, kind=%s, mode=%d) -> %08X (%s+0x%X)", in.Addr, fc.Fn.Name, kindName(in.Kind), in.TMode, tgt, target.Fn.Name, tgt-target.Fn.Addr))
					}
				}
			}
			for _, addr := range fc.Order {
				in := fc.Insts[addr]
				switch in.Kind {
				case KBranch, KCondBranch:
					if in.Target < fc.Fn.Addr || in.Target >= fc.End {
						resolve(in, in.Target, false)
					}
				case KBranchLink:
					resolve(in, in.Target, true)
				}
				if !isTerminator(in) && addr+in.Size >= fc.End {
					a.Stats["fallthrough-exit"]++
					resolve(in, addr+in.Size, false)
				}
				a.sweepAddressTaken(fc, in, dirty, func(addr uint32, mode Mode) {
					synths = append(synths, synthReq{addr, mode})
				})
			}
		}
		for _, s := range synths {
			if a.ByAddr[s.addr] == nil {
				dirty[a.synthesize(s.addr, s.mode)] = true
			}
		}
		if len(dirty) == 0 {
			break
		}
		a.Stats["cross-entry-rounds"]++
		for fc := range dirty {
			sort.Slice(fc.Entries, func(i, j int) bool { return fc.Entries[i].Addr < fc.Entries[j].Addr })
			walk(im, fc, a.Stats)
			for fc.overlaps() {
				dropped := false
				for i := len(fc.Entries) - 1; i >= 0; i-- {
					if fc.Entries[i].FromPool {
						a.Stats["pool-entry-dropped"]++
						a.Rejected[fc.Entries[i].Addr] = true
						fc.Entries = append(fc.Entries[:i], fc.Entries[i+1:]...)
						dropped = true
						break
					}
				}
				if !dropped {
					panic(fmt.Sprintf("unresolvable overlap in %s", fc.Fn.Name))
				}
				walk(im, fc, a.Stats)
			}
		}
	}

	for _, fc := range a.Fns {
		a.Stats["insts"] += len(fc.Insts)
		a.Stats["entries"] += len(fc.Entries)
		for _, addr := range fc.Order {
			a.Stats["kind."+kindName(fc.Insts[addr].Kind)]++
		}
		a.Stats["traps"] += len(fc.Traps)
	}

	return a
}

func (a *Analysis) sweepAddressTaken(fc *FnCode, in *Inst, dirty map[*FnCode]bool, synth func(uint32, Mode)) {
	if in.Kind != KGeneric {
		return
	}
	var w uint32
	verified := false
	switch {
	case in.Mode == ModeThumb && in.Raw>>11 == 0b01001:
		lit := ((in.Addr + 4) &^ 2) + (in.Raw&0xFF)<<2
		if !a.Im.readable(lit) {
			return
		}
		w = a.Im.Read32(lit)
	case in.Mode == ModeThumb && in.Raw>>11 == 0b10100:
		rd := in.Raw >> 8 & 7
		bxWanted := 0x4700 | rd<<3
		for scan := in.Addr + 2; scan < in.Addr+10 && scan < fc.End; scan += 2 {
			if uint32(a.Im.Read16(scan)) == bxWanted {
				verified = true
				break
			}
		}
		if !verified {
			return
		}
		w = ((in.Addr + 4) &^ 2) + (in.Raw&0xFF)<<2
	case in.Mode == ModeArm && in.Raw&0x0F7F0000 == 0x051F0000:
		off := in.Raw & 0xFFF
		lit := in.Addr + 8 - off
		if in.Raw>>23&1 == 1 {
			lit = in.Addr + 8 + off
		}
		if !a.Im.readable(lit) {
			return
		}
		w = a.Im.Read32(lit)
	case in.Mode == ModeArm && in.Raw&0x0FFF0000 == 0x028F0000:
		imm := in.Raw & 0xFF
		rot := (in.Raw >> 8 & 0xF) * 2
		w = in.Addr + 8 + (imm>>rot | imm<<(32-rot))
		if rd := in.Raw >> 12 & 0xF; rd != 14 {
			for scan := in.Addr + 4; scan < in.Addr+16 && scan+4 <= fc.End; scan += 4 {
				word := a.Im.Read32(scan)
				if word&0x0FFFFFF0 == 0x012FFF10 && word&0xF == rd {
					verified = true
					break
				}
			}
			if !verified {
				return
			}
		}
	default:
		return
	}
	tgt := w &^ 1
	if !a.Im.InCode(tgt) || a.ByAddr[tgt] != nil {
		return
	}
	mode := ModeArm
	if w&1 == 1 {
		mode = ModeThumb
	} else if w&3 != 0 {
		return
	}
	if a.Rejected[tgt] {
		return
	}
	cf := a.containing(tgt)
	if cf == nil {
		if verified {
			synth(tgt, mode)
		}
		return
	}
	if strings.HasPrefix(cf.Fn.Name, "stub_") {
		return
	}
	if mode == ModeArm && cf.Fn.Thumb && !verified {
		return
	}
	if !cf.hasEntry(tgt) {
		cf.Entries = append(cf.Entries, Entry{tgt, mode, true})
		dirty[cf] = true
		a.Stats["address-taken-entry"]++
		if len(a.Samples["at-entry"]) < 20 {
			a.Samples["at-entry"] = append(a.Samples["at-entry"], fmt.Sprintf("%08X (via pool at %08X in %s) -> %s+0x%X", tgt, in.Addr, fc.Fn.Name, cf.Fn.Name, tgt-cf.Fn.Addr))
		}
	}
}

func (fc *FnCode) overlaps() bool {
	var prevEnd uint32
	for _, addr := range fc.Order {
		if addr < prevEnd {
			return true
		}
		prevEnd = addr + fc.Insts[addr].Size
	}
	return false
}

func (a *Analysis) synthesize(addr uint32, mode Mode) *FnCode {
	a.Stats["synthesized"]++
	i := sort.Search(len(a.Fns), func(i int) bool { return a.Fns[i].Fn.Addr > addr })
	end := addr + 4
	if i < len(a.Fns) {
		end = a.Fns[i].Fn.Addr
	}
	fn := Fn{
		Name:  fmt.Sprintf("stub_%08X", addr),
		Addr:  addr,
		Size:  end - addr,
		Thumb: mode == ModeThumb,
	}
	fc := &FnCode{Fn: fn, End: end}
	a.Fns = append(a.Fns, fc)
	sort.Slice(a.Fns, func(i, j int) bool { return a.Fns[i].Fn.Addr < a.Fns[j].Fn.Addr })
	a.ByAddr[addr] = fc
	if len(a.Samples["synthesized"]) < 15 {
		a.Samples["synthesized"] = append(a.Samples["synthesized"], fmt.Sprintf("%08X size=%d mode=%d", addr, fn.Size, mode))
	}
	return fc
}

func kindName(k InstKind) string {
	return [...]string{"generic", "condbranch", "branch", "bl", "bx", "bxpc", "movpc", "poppc", "swi", "ldrpc-static", "ldmpc", "alupc", "trap", "farjump"}[k]
}

func (a *Analysis) containing(addr uint32) *FnCode {
	i := sort.Search(len(a.Fns), func(i int) bool { return a.Fns[i].Fn.Addr > addr })
	if i == 0 {
		return nil
	}
	fc := a.Fns[i-1]
	if addr >= fc.Fn.Addr && addr < fc.End {
		return fc
	}
	return nil
}

func walk(im *Image, fc *FnCode, stats map[string]int) {
	fc.Insts = map[uint32]*Inst{}
	fc.Labels = map[uint32]bool{}
	fc.Prov = map[uint32]uint32{}

	type wi struct {
		addr uint32
		mode Mode
		from uint32
	}
	mode := ModeThumb
	if !fc.Fn.Thumb {
		mode = ModeArm
	}
	work := []wi{{fc.Fn.Addr, mode, 0}}
	for _, e := range fc.Entries {
		work = append(work, wi{e.Addr, e.Mode, 1})
		fc.Labels[e.Addr] = true
	}

	var cur uint32
	push := func(addr uint32, m Mode) {
		work = append(work, wi{addr, m, cur})
	}

	for len(work) > 0 {
		w := work[len(work)-1]
		work = work[:len(work)-1]
		if _, ok := fc.Insts[w.addr]; ok {
			continue
		}
		if w.addr < fc.Fn.Addr || w.addr >= fc.End || !im.InCode(w.addr) {
			continue
		}

		cur = w.addr
		in := decode(im, w.addr, w.mode)
		in.TMode = w.mode
		fc.Insts[w.addr] = in
		fc.Prov[w.addr] = w.from

		switch in.Kind {
		case KCondBranch:
			if in.Target >= fc.Fn.Addr && in.Target < fc.End {
				fc.Labels[in.Target] = true
				push(in.Target, w.mode)
			}
			push(w.addr+in.Size, w.mode)
		case KBranch:
			if in.Target >= fc.Fn.Addr && in.Target < fc.End {
				fc.Labels[in.Target] = true
				push(in.Target, w.mode)
			}
			if !isTerminator(in) {
				push(w.addr+in.Size, w.mode)
			}
		case KBranchLink:
			if _, labeled := im.Labels[in.Target]; !labeled && in.Target >= fc.Fn.Addr && in.Target < fc.End {
				in.Kind = KFarJump
				fc.Labels[in.Target] = true
				push(in.Target, w.mode)
				break
			}
			push(w.addr+in.Size, w.mode)
		case KBXPC:
			t := (w.addr + 4) &^ 3
			if w.mode == ModeArm {
				t = (w.addr + 8) &^ 3
			}
			in.Kind = KBranch
			in.Target = t
			in.TMode = ModeArm
			if t >= fc.Fn.Addr && t < fc.End {
				fc.Labels[t] = true
				push(t, ModeArm)
			} else {
				stats["bxpc-cross"]++
			}
		case KBX, KPopPC, KLdmPC, KAluPC, KTrap, KLdrPCStatic:
			if !isTerminator(in) {
				push(w.addr+in.Size, w.mode)
			}
		case KMovPC:
			targets := extractJumpTable(im, fc, w.addr)
			if len(targets) == 0 {
				stats["movpc-notable"]++
				fc.Traps = append(fc.Traps, fmt.Sprintf("movpc-notable at %08X in %s", w.addr, fc.Fn.Name))
			}
			for _, t := range targets {
				if t >= fc.Fn.Addr && t < fc.End {
					fc.Labels[t] = true
					fc.JTTargets = append(fc.JTTargets, t)
					push(t, w.mode)
				}
			}
		case KSWI:
			push(w.addr+in.Size, w.mode)
		default:
			push(w.addr+in.Size, w.mode)
		}
	}

	fc.Order = make([]uint32, 0, len(fc.Insts))
	for addr := range fc.Insts {
		fc.Order = append(fc.Order, addr)
	}
	sort.Slice(fc.Order, func(i, j int) bool { return fc.Order[i] < fc.Order[j] })

	seen := map[uint32]bool{}
	var jt []uint32
	for _, t := range fc.JTTargets {
		if !seen[t] {
			seen[t] = true
			jt = append(jt, t)
		}
	}
	fc.JTTargets = jt
	sort.Slice(fc.JTTargets, func(i, j int) bool { return fc.JTTargets[i] < fc.JTTargets[j] })
}

func otherMode(m Mode) Mode {
	if m == ModeThumb {
		return ModeArm
	}
	return ModeThumb
}

func extractJumpTable(im *Image, fc *FnCode, movAddr uint32) []uint32 {
	movReg := uint32(im.Read16(movAddr)) >> 3 & 0xF

	var baseReg uint32
	loadAddr := uint32(0)
	for addr := movAddr - 2; addr+16 > movAddr && addr >= fc.Fn.Addr; addr -= 2 {
		raw := uint32(im.Read16(addr))
		if raw>>9 == 0b0101100 && raw&7 == movReg {
			baseReg = raw >> 3 & 7
			loadAddr = addr
			break
		}
	}
	if loadAddr == 0 {
		return nil
	}

	base := resolveLiteral(im, fc, loadAddr, baseReg)
	if base == 0 || base&3 != 0 || !im.readable(base) {
		return nil
	}

	var targets []uint32
	for i := uint32(0); i < 1024; i++ {
		p := base + i*4
		if !im.readable(p) {
			break
		}
		t := im.Read32(p) &^ 1
		if t < fc.Fn.Addr || t >= fc.End {
			break
		}
		targets = append(targets, t)
	}
	return targets
}

func resolveLiteral(im *Image, fc *FnCode, from uint32, reg uint32) uint32 {
	spSlot := int64(-1)
	for addr := from - 2; addr >= fc.Fn.Addr; addr -= 2 {
		raw := uint32(im.Read16(addr))
		if spSlot >= 0 {
			if raw>>11 == 0b10010 && int64(raw&0xFF) == spSlot {
				spSlot = -1
				reg = raw >> 8 & 7
			}
			continue
		}
		if reg < 8 && raw>>11 == 0b01001 && raw>>8&7 == reg {
			litAddr := ((addr + 4) &^ 2) + (raw&0xFF)<<2
			if !im.readable(litAddr) {
				return 0
			}
			return im.Read32(litAddr)
		}
		if reg < 8 && raw>>11 == 0b10011 && raw>>8&7 == reg {
			spSlot = int64(raw & 0xFF)
			continue
		}
		if raw>>8 == 0b01000110 {
			rd := raw&7 | raw>>7&1<<3
			if rd == reg {
				reg = raw >> 3 & 0xF
				continue
			}
		}
		if reg < 8 && raw>>6 == 0 && raw&7 == reg {
			reg = raw >> 3 & 7
		}
	}
	return 0
}

func (im *Image) readable(addr uint32) bool {
	for _, s := range im.segs {
		if addr >= s.start && addr+4 <= s.start+uint32(len(s.data)) {
			return true
		}
	}
	return false
}

var _ = fmt.Sprintf

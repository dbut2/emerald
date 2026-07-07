package main

import (
	"debug/elf"
	"encoding/binary"
	"fmt"
	"sort"
	"strings"
)

type Fn struct {
	Name  string
	Addr  uint32
	Size  uint32
	Thumb bool
}

type Image struct {
	Entry    uint32
	Funcs    []Fn
	Labels   map[uint32]string
	segs     []seg
	codeSegs []seg
}

type seg struct {
	start uint32
	data  []byte
}

func (im *Image) Read16(addr uint32) uint16 {
	b := im.bytes(addr, 2)
	return binary.LittleEndian.Uint16(b)
}

func (im *Image) Read32(addr uint32) uint32 {
	b := im.bytes(addr, 4)
	return binary.LittleEndian.Uint32(b)
}

func (im *Image) bytes(addr, n uint32) []byte {
	for _, s := range im.segs {
		if addr >= s.start && addr+n <= s.start+uint32(len(s.data)) {
			return s.data[addr-s.start:]
		}
	}
	panic(fmt.Sprintf("read outside image: %08X", addr))
}

func (im *Image) InCode(addr uint32) bool {
	for _, s := range im.codeSegs {
		if addr >= s.start && addr < s.start+uint32(len(s.data)) {
			return true
		}
	}
	return false
}

func loadImage(path string) (*Image, error) {
	f, err := elf.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	im := &Image{Entry: uint32(f.Entry)}

	codeSections := map[string]bool{".text": true, "lib_text": true}
	for _, s := range f.Sections {
		if s.Type != elf.SHT_PROGBITS || s.Addr < 0x08000000 {
			continue
		}
		data, err := s.Data()
		if err != nil {
			return nil, err
		}
		sg := seg{start: uint32(s.Addr), data: data}
		im.segs = append(im.segs, sg)
		if codeSections[s.Name] {
			im.codeSegs = append(im.codeSegs, sg)
		}
	}
	if len(im.codeSegs) != 2 {
		return nil, fmt.Errorf("expected .text and lib_text, got %d code sections", len(im.codeSegs))
	}

	syms, err := f.Symbols()
	if err != nil {
		return nil, err
	}
	im.Labels = map[uint32]string{}
	for _, sym := range syms {
		if a := uint32(sym.Value) &^ 1; im.InCode(a) && sym.Name != "" {
			im.Labels[a] = sym.Name
		}
		if elf.ST_TYPE(sym.Info) != elf.STT_FUNC {
			continue
		}
		addr := uint32(sym.Value)
		thumb := addr&1 == 1
		addr &^= 1
		if !im.InCode(addr) {
			continue
		}
		im.Funcs = append(im.Funcs, Fn{
			Name:  sym.Name,
			Addr:  addr,
			Size:  uint32(sym.Size),
			Thumb: thumb,
		})
	}

	sort.Slice(im.Funcs, func(i, j int) bool { return im.Funcs[i].Addr < im.Funcs[j].Addr })

	covered := func(addr uint32) bool {
		i := sort.Search(len(im.Funcs), func(i int) bool { return im.Funcs[i].Addr > addr })
		if i == 0 {
			return false
		}
		fn := im.Funcs[i-1]
		return addr >= fn.Addr && (addr == fn.Addr || addr < fn.Addr+fn.Size)
	}
	for _, sym := range syms {
		if elf.ST_TYPE(sym.Info) == elf.STT_FUNC || elf.ST_BIND(sym.Info) != elf.STB_GLOBAL || sym.Name == "" {
			continue
		}
		if elf.ST_TYPE(sym.Info) == elf.STT_OBJECT {
			continue
		}
		addr := uint32(sym.Value) &^ 1
		if !im.InCode(addr) || covered(addr) || addr < 0x08000100 {
			continue
		}
		thumb := addr%4 == 2 || strings.Contains(sym.Name, "_from_thumb") || strings.HasSuffix(sym.Name, "_veneer")
		fmt.Printf("orphan code symbol: %s @%08X thumb=%v\n", sym.Name, addr, thumb)
		im.Funcs = append(im.Funcs, Fn{
			Name:  sym.Name,
			Addr:  addr,
			Thumb: thumb,
		})
	}
	sort.Slice(im.Funcs, func(i, j int) bool { return im.Funcs[i].Addr < im.Funcs[j].Addr })

	dedup := im.Funcs[:0]
	for _, fn := range im.Funcs {
		if len(dedup) > 0 && dedup[len(dedup)-1].Addr == fn.Addr {
			continue
		}
		dedup = append(dedup, fn)
	}
	im.Funcs = dedup

	for i := range im.Funcs {
		if im.Funcs[i].Size != 0 {
			continue
		}
		if i+1 < len(im.Funcs) {
			im.Funcs[i].Size = im.Funcs[i+1].Addr - im.Funcs[i].Addr
		}
	}

	return im, nil
}

package main

func signext(v uint32, bits uint) int32 {
	shift := 32 - bits
	return int32(v<<shift) >> shift
}

func decode(im *Image, addr uint32, mode Mode) *Inst {
	if mode == ModeThumb {
		return decodeThumb(im, addr)
	}
	return decodeArm(im, addr)
}

func decodeThumb(im *Image, addr uint32) *Inst {
	raw := uint32(im.Read16(addr))
	in := &Inst{Addr: addr, Mode: ModeThumb, Raw: raw, Size: 2, Kind: KGeneric}

	switch {
	case raw>>8 == 0xDF:
		in.Kind = KSWI

	case raw>>12 == 0xD && (raw>>8)&0xF < 0xE:
		in.Kind = KCondBranch
		in.Cond = (raw >> 8) & 0xF
		in.Target = uint32(int64(addr) + 4 + int64(signext(raw&0xFF, 8)<<1))

	case raw>>11 == 0b11100:
		in.Kind = KBranch
		in.Target = uint32(int64(addr) + 4 + int64(signext(raw&0x7FF, 11)<<1))

	case raw>>11 == 0b11110:
		lo := uint32(im.Read16(addr + 2))
		if lo>>11 != 0b11111 {
			in.Kind = KTrap
			return in
		}
		in.Size = 4
		in.Kind = KBranchLink
		off := int64(signext(raw&0x7FF, 11)) << 12
		in.Target = uint32(int64(addr) + 4 + off + int64((lo&0x7FF)<<1))

	case raw>>11 == 0b11111:
		in.Kind = KTrap

	case raw>>7 == 0b010001110:
		rm := (raw >> 3) & 0xF
		if rm == 15 {
			in.Kind = KBXPC
		} else {
			in.Kind = KBX
			in.Reg = rm
		}

	case raw>>7 == 0b010001111:
		in.Kind = KTrap

	case raw>>8 == 0b01000110 && raw&0x87 == 0x87:
		in.Kind = KMovPC
		in.Reg = (raw >> 3) & 0xF

	case raw>>8 == 0b01000100 && raw&0x87 == 0x87:
		in.Kind = KTrap

	case raw>>8 == 0xBD:
		in.Kind = KPopPC
		in.Reg = raw & 0xFF

	default:
		in.ReadsPC = thumbReadsPC(raw)
	}
	return in
}

func thumbReadsPC(raw uint32) bool {
	switch {
	case raw>>11 == 0b01001:
		return true
	case raw>>11 == 0b10100:
		return true
	case raw>>10 == 0b010001:
		rm := (raw >> 3) & 0xF
		rd := (raw & 7) | (raw>>7&1)<<3
		return rm == 15 || rd == 15
	}
	return false
}

func decodeArm(im *Image, addr uint32) *Inst {
	raw := im.Read32(addr)
	in := &Inst{Addr: addr, Mode: ModeArm, Raw: raw, Size: 4, Kind: KGeneric, Cond: raw >> 28}
	in.ReadsPC = true

	op := raw >> 25 & 7

	switch {
	case in.Cond == 0xF:
		in.Kind = KTrap

	case op == 0b101:
		if raw>>24&1 == 1 {
			in.Kind = KBranchLink
		} else {
			in.Kind = KBranch
		}
		in.Target = uint32(int64(addr) + 8 + int64(signext(raw&0xFFFFFF, 24)<<2))

	case raw&0x0FFFFFF0 == 0x012FFF10:
		rm := raw & 0xF
		if rm == 15 {
			in.Kind = KBXPC
		} else {
			in.Kind = KBX
			in.Reg = rm
		}

	case raw&0x0F000000 == 0x0F000000:
		in.Kind = KSWI

	case op == 0b100 && raw>>20&1 == 1 && raw>>15&1 == 1:
		in.Kind = KLdmPC

	case op&0b110 == 0b010 && raw>>20&1 == 1 && raw>>12&0xF == 15:
		if raw&0x0FFF0FFF == 0x051F0004 {
			in.Kind = KLdrPCStatic
			in.Target = im.Read32(addr + 8 - 4)
		} else {
			in.Kind = KTrap
		}

	case op&0b110 == 0 && raw>>12&0xF == 15 && !isArmPsrOrMisc(raw):
		in.Kind = KAluPC

	default:
	}
	return in
}

func isArmPsrOrMisc(raw uint32) bool {
	if raw&0x0FBF0FFF == 0x010F0000 {
		return true
	}
	if raw&0x0DB0F000 == 0x0120F000 {
		return true
	}
	if raw&0x0FB00FF0 == 0x01000090 {
		return true
	}
	if raw&0x0F0000F0 == 0x00000090 {
		return true
	}
	if raw&0x0E000090 == 0x00000090 {
		return true
	}
	return false
}

package main

import (
	"fmt"
	"strings"
)

// thumbSemantic lifts a single 16-bit THUMB data instruction to Go statements
// operating directly on CPU state, mirroring gba's interpreter exactly.
// Returns ok=false for encodings left to the c.Thumb(raw) fallback.
func thumbSemantic(raw, addr uint32) (string, bool) {
	bit := func(b uint) uint32 { return raw >> b & 1 }
	bits := func(lo, n uint) uint32 { return raw >> lo & (1<<n - 1) }

	var b strings.Builder
	line := func(f string, a ...any) { fmt.Fprintf(&b, "\t"+f+"\n", a...) }
	nzFrom := func(v string) string {
		return fmt.Sprintf("int32(%s) < 0, %s == 0", v, v)
	}

	switch {
	case raw>>13 == 0b000 && raw>>11 != 0b00011: // shift by immediate
		op := bits(11, 2)
		off := bits(6, 5)
		rs, rd := bits(3, 3), bits(0, 3)
		switch op {
		case 0b00: // LSL
			if off == 0 {
				line("c.R[%d] = c.R[%d]", rd, rs)
				line("c.SetNZ(%s)", nzFrom(fmt.Sprintf("c.R[%d]", rd)))
			} else {
				line("{")
				line("\tv, cy := g.ShiftLSL(c.R[%d], %d)", rs, off)
				line("\tc.R[%d] = v", rd)
				line("\tc.SetNZC(int32(v) < 0, v == 0, cy)")
				line("}")
			}
		case 0b01, 0b10: // LSR / ASR
			amt := off
			if amt == 0 {
				amt = 32
			}
			fn := "g.ShiftLSR"
			if op == 0b10 {
				fn = "g.ShiftASR"
			}
			line("{")
			line("\tv, cy := %s(c.R[%d], %d)", fn, rs, amt)
			line("\tc.R[%d] = v", rd)
			line("\tc.SetNZC(int32(v) < 0, v == 0, cy)")
			line("}")
		default:
			return "", false
		}

	case raw>>11 == 0b00011: // ADD/SUB register or 3-bit immediate
		imm := bit(10)
		sub := bit(9)
		rd, rs := bits(0, 3), bits(3, 3)
		var op2 string
		if imm == 1 {
			op2 = fmt.Sprintf("%d", bits(6, 3))
		} else {
			op2 = fmt.Sprintf("c.R[%d]", bits(6, 3))
		}
		fn, flag := "g.ADD", "g.FlagArithAdd"
		if sub == 1 {
			fn, flag = "g.SUB", "g.FlagArithSub"
		}
		line("{")
		line("\top1, op2 := c.R[%d], uint32(%s)", rs, op2)
		line("\tval := %s(op1, op2, 0)", fn)
		line("\tc.R[%d] = uint32(val)", rd)
		line("\tc.SetNZCV(%s(op1, op2, val))", flag)
		line("}")

	case raw>>13 == 0b001: // MOV/CMP/ADD/SUB 8-bit immediate
		op := bits(11, 2)
		rd := bits(8, 3)
		nn := bits(0, 8)
		switch op {
		case 0b00: // MOV
			line("c.R[%d] = %d", rd, nn)
			line("c.SetNZ(%s)", nzFrom(fmt.Sprintf("%d", nn)))
		case 0b01: // CMP
			line("{")
			line("\tval := g.SUB(c.R[%d], %d, 0)", rd, nn)
			line("\tc.SetNZCV(g.FlagArithSub(c.R[%d], %d, val))", rd, nn)
			line("}")
		case 0b10: // ADD
			line("{")
			line("\tval := g.ADD(c.R[%d], %d, 0)", rd, nn)
			line("\tc.SetNZCV(g.FlagArithAdd(c.R[%d], %d, val))", rd, nn)
			line("\tc.R[%d] = uint32(val)", rd)
			line("}")
		case 0b11: // SUB
			line("{")
			line("\tval := g.SUB(c.R[%d], %d, 0)", rd, nn)
			line("\tc.SetNZCV(g.FlagArithSub(c.R[%d], %d, val))", rd, nn)
			line("\tc.R[%d] = uint32(val)", rd)
			line("}")
		}

	case raw>>10 == 0b010000: // ALU
		if !thumbALU(&b, raw) {
			return "", false
		}

	case raw>>10 == 0b010001: // hi-register ADD/CMP/MOV (non-PC, non-BX)
		op := bits(8, 2)
		rd := bits(0, 3) + bit(7)<<3
		rs := bits(3, 3) + bit(6)<<3
		switch op {
		case 0b00: // ADD
			line("c.R[%d] += c.R[%d]", rd, rs)
		case 0b01: // CMP
			line("{")
			line("\tval := g.SUB(c.R[%d], c.R[%d], 0)", rd, rs)
			line("\tc.SetNZCV(g.FlagArithSub(c.R[%d], c.R[%d], val))", rd, rs)
			line("}")
		case 0b10: // MOV
			line("c.R[%d] = c.R[%d]", rd, rs)
		default:
			return "", false
		}

	case raw>>11 == 0b01001: // LDR Rd, [PC, #nn]
		rd := bits(8, 3)
		a := (addr+4)&^2 + bits(0, 8)<<2
		line("c.R[%d] = c.Memory.Read32(0x%08X, true, false)", rd, a)

	case raw>>12 == 0b0101 && bit(9) == 0: // load/store register offset
		op := bits(10, 2)
		rd, rb, ro := bits(0, 3), bits(3, 3), bits(6, 3)
		ea := fmt.Sprintf("c.R[%d]+c.R[%d]", rb, ro)
		switch op {
		case 0b00: // STR
			line("c.Memory.Set32(%s, c.R[%d], true, false)", ea, rd)
		case 0b01: // STRB
			line("c.Memory.Set8(%s, uint8(c.R[%d]), true, false)", ea, rd)
		case 0b10: // LDR
			line("c.R[%d] = c.Memory.Read32(%s, true, false)", rd, ea)
		case 0b11: // LDRB
			line("c.R[%d] = uint32(c.Memory.Read8(%s, true, false))", rd, ea)
		}

	case raw>>12 == 0b0101 && bit(9) == 1: // load/store halfword/sign, register offset
		op := bits(10, 2)
		rd, rb, ro := bits(0, 3), bits(3, 3), bits(6, 3)
		ea := fmt.Sprintf("c.R[%d]+c.R[%d]", rb, ro)
		switch op {
		case 0b00: // STRH
			line("c.Memory.Set16(%s, uint16(c.R[%d]), true, false)", ea, rd)
		case 0b01: // LDSB
			line("c.R[%d] = uint32(int32(int8(c.Memory.Read8(%s, true, false))))", rd, ea)
		case 0b10: // LDRH
			line("c.R[%d] = c.LoadHalf(%s)", rd, ea)
		case 0b11: // LDSH
			line("c.R[%d] = c.LoadHalfSigned(%s)", rd, ea)
		}

	case raw>>13 == 0b011: // load/store immediate offset
		op := bits(11, 2)
		nn := bits(6, 5)
		rb, rd := bits(3, 3), bits(0, 3)
		switch op {
		case 0b00: // STR
			line("c.Memory.Set32(c.R[%d]+%d, c.R[%d], true, false)", rb, nn<<2, rd)
		case 0b01: // LDR
			line("c.R[%d] = c.Memory.Read32(c.R[%d]+%d, true, false)", rd, rb, nn<<2)
		case 0b10: // STRB
			line("c.Memory.Set8(c.R[%d]+%d, uint8(c.R[%d]), true, false)", rb, nn, rd)
		case 0b11: // LDRB
			line("c.R[%d] = uint32(c.Memory.Read8(c.R[%d]+%d, true, false))", rd, rb, nn)
		}

	case raw>>12 == 0b1000: // load/store halfword immediate
		store := bit(11) == 0
		nn := bits(6, 5) << 1
		rb, rd := bits(3, 3), bits(0, 3)
		if store {
			line("c.Memory.Set16(c.R[%d]+%d, uint16(c.R[%d]), true, false)", rb, nn, rd)
		} else {
			line("c.R[%d] = c.LoadHalf(c.R[%d]+%d)", rd, rb, nn)
		}

	case raw>>12 == 0b1001: // load/store SP-relative
		load := bit(11) == 1
		rd := bits(8, 3)
		nn := bits(0, 8) << 2
		if load {
			line("c.R[%d] = c.Memory.Read32(c.R[13]+%d, true, false)", rd, nn)
		} else {
			line("c.Memory.Set32(c.R[13]+%d, c.R[%d], true, false)", nn, rd)
		}

	case raw>>12 == 0b1010: // ADR (PC-relative) / ADD SP
		rd := bits(8, 3)
		nn := bits(0, 8) << 2
		if bit(11) == 0 {
			line("c.R[%d] = 0x%08X", rd, (addr+4)&^2+nn)
		} else {
			line("c.R[%d] = c.R[13] + %d", rd, nn)
		}

	case raw>>8 == 0b10110000: // ADD/SUB SP, #nn
		nn := bits(0, 7) << 2
		if bit(7) == 0 {
			line("c.R[13] += %d", nn)
		} else {
			line("c.R[13] -= %d", nn)
		}

	case raw&0xF600 == 0xB400: // PUSH / POP (without PC)
		if !thumbPushPop(&b, raw) {
			return "", false
		}

	case raw>>12 == 0b1100: // STMIA / LDMIA
		if !thumbBlock(&b, raw) {
			return "", false
		}

	default:
		return "", false
	}

	return b.String(), true
}

func thumbALU(b *strings.Builder, raw uint32) bool {
	op := raw >> 6 & 0xF
	rs := raw >> 3 & 7
	rd := raw & 7
	line := func(f string, a ...any) { fmt.Fprintf(b, "\t"+f+"\n", a...) }
	nz := func() { line("\tc.SetNZ(int32(v) < 0, v == 0)") }

	logic := func(expr string) {
		line("{")
		line("\tv := uint32(%s)", expr)
		line("\tc.R[%d] = v", rd)
		nz()
		line("}")
	}
	arith := func(expr, flag, l, r string) {
		line("{")
		line("\tval := %s", expr)
		line("\tv := uint32(val)")
		line("\tc.R[%d] = v", rd)
		line("\tc.SetNZCV(%s(%s, %s, val))", flag, l, r)
		line("}")
	}
	shift := func(fn string) {
		line("{")
		line("\tamount := c.R[%d] & 0xFF", rs)
		line("\tv, carry := %s(c.R[%d], amount)", fn, rd)
		line("\tc.R[%d] = v", rd)
		line("\tif amount > 0 {")
		line("\t\tc.SetNZC(int32(v) < 0, v == 0, carry)")
		line("\t} else {")
		line("\t\tc.SetNZ(int32(v) < 0, v == 0)")
		line("\t}")
		line("}")
	}

	l := fmt.Sprintf("c.R[%d]", rd)
	r := fmt.Sprintf("c.R[%d]", rs)
	switch op {
	case 0b0000: // AND
		logic(fmt.Sprintf("%s & %s", l, r))
	case 0b0001: // EOR
		logic(fmt.Sprintf("%s ^ %s", l, r))
	case 0b0010: // LSL
		shift("g.ShiftLSL")
	case 0b0011: // LSR
		shift("g.ShiftLSR")
	case 0b0100: // ASR
		shift("g.ShiftASR")
	case 0b0101: // ADC
		arith(fmt.Sprintf("g.ADC(%s, %s, c.CFlag())", l, r), "g.FlagArithAdd", l, r)
	case 0b0110: // SBC
		arith(fmt.Sprintf("g.SBCThumb(%s, %s, c.CFlag())", l, r), "g.FlagArithSub", l, r)
	case 0b0111: // ROR
		shift("g.ShiftROR")
	case 0b1000: // TST
		line("{")
		line("\tv := %s & %s", l, r)
		nz()
		line("}")
	case 0b1001: // NEG
		arith(fmt.Sprintf("g.SUB(0, %s, 0)", r), "g.FlagArithSub", "0", r)
	case 0b1010: // CMP
		line("{")
		line("\tval := g.SUB(%s, %s, 0)", l, r)
		line("\tc.SetNZCV(g.FlagArithSub(%s, %s, val))", l, r)
		line("}")
	case 0b1011: // CMN
		line("{")
		line("\tval := g.ADD(%s, %s, 0)", l, r)
		line("\tc.SetNZCV(g.FlagArithAdd(%s, %s, val))", l, r)
		line("}")
	case 0b1100: // ORR
		logic(fmt.Sprintf("%s | %s", l, r))
	case 0b1101: // MUL
		arith(fmt.Sprintf("g.MUL(%s, %s, 0)", l, r), "g.FlagArithAdd", l, r)
	case 0b1110: // BIC
		logic(fmt.Sprintf("%s &^ %s", l, r))
	case 0b1111: // MVN
		logic(fmt.Sprintf("^%s", r))
	default:
		return false
	}
	return true
}

func thumbPushPop(b *strings.Builder, raw uint32) bool {
	line := func(f string, a ...any) { fmt.Fprintf(b, "\t"+f+"\n", a...) }
	extra := raw >> 8 & 1
	rlist := raw & 0xFF
	if raw>>11&1 == 0 { // PUSH
		line("{")
		if extra == 1 {
			line("\tc.R[13] -= 4")
			line("\tc.Memory.Set32(c.R[13], c.R[14], true, false)")
		}
		for i := 7; i >= 0; i-- {
			if rlist>>uint(i)&1 == 1 {
				line("\tc.R[13] -= 4")
				line("\tc.Memory.Set32(c.R[13], c.R[%d], true, false)", i)
			}
		}
		line("}")
		return true
	}
	// POP (PC form is handled as control flow elsewhere)
	if extra == 1 {
		return false
	}
	line("{")
	for i := 0; i <= 7; i++ {
		if rlist>>uint(i)&1 == 1 {
			line("\tc.R[%d] = c.Memory.Read32(c.R[13], true, false)", i)
			line("\tc.R[13] += 4")
		}
	}
	line("}")
	return true
}

func thumbBlock(b *strings.Builder, raw uint32) bool {
	line := func(f string, a ...any) { fmt.Fprintf(b, "\t"+f+"\n", a...) }
	rb := raw >> 8 & 7
	rlist := raw & 0xFF
	if rlist == 0 { // empty-list edge case loads/stores PC; leave to interpreter
		return false
	}
	n := uint32(0)
	for i := uint(0); i < 8; i++ {
		n += rlist >> i & 1
	}
	final := n * 4

	line("{")
	line("\taddr := c.R[%d]", rb)
	if raw>>11&1 == 0 { // STMIA
		first := true
		for i := uint(0); i < 8; i++ {
			if rlist>>i&1 == 0 {
				continue
			}
			if uint32(i) == rb && !first {
				line("\tc.Memory.Set32(addr, c.R[%d]+%d, true, false)", rb, final)
			} else {
				line("\tc.Memory.Set32(addr, c.R[%d], true, false)", i)
			}
			line("\taddr += 4")
			first = false
		}
		line("\tc.R[%d] = addr", rb)
	} else { // LDMIA
		for i := uint(0); i < 8; i++ {
			if rlist>>i&1 == 0 {
				continue
			}
			line("\tc.R[%d] = c.Memory.Read32(addr, true, false)", i)
			line("\taddr += 4")
		}
		if rlist>>rb&1 == 0 {
			line("\tc.R[%d] = addr", rb)
		}
	}
	line("}")
	return true
}

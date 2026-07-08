# Native pokeemerald-in-Go port — design record

## Goal
Run pokeemerald's game logic as native host machine code, driven from Go over a
per-frame cgo boundary. pret/pokeemerald is vendored as a squashed git subtree
under `pokeemerald/` (pinned at upstream `83df84e40`). All porting code lives
out-of-tree under `port/` behind header-shadowing, so subtree pulls stay clean.

## Validated so far
- Toolchain: Go 1.26 (cgo), Apple clang 21, `arm-none-eabi-gcc` present (oracle).
- `make modern` builds the full asset + generated-header substrate and an oracle
  ROM (`pokeemerald/pokeemerald_modern.gba`).
- Host compile works through pokeemerald's own pipeline: `clang -E → preproc →
  clang -c`, with `port/include` shadowing ahead of `pokeemerald/include`.
  Driver: `port/hostcc.sh`; whole-tree sweep: `port/sweep.sh`.
- Sweep result: **304 / 310 src/*.c compile to host arm64 objects.** The 6
  failures split into: asm/boot (`librfu_intr`, `m4a`, `multiboot`,
  `rom_header_gf` — replace/stub anyway) and LP64 struct-size asserts
  (`list_menu`, `save`).

## Decision: 64-bit arm64-native + a pointer-width portability shim (Path B)
LP64 (`void* = 8 B`) breaks two things vs the GBA's ILP32:
1. Pointers packed into fixed `s16` task/sprite data via `(u32)` truncate — the
   high 32 bits are lost at the cast, before storage. On macOS arm64 heap
   addresses exceed 4 GB, so this is fatal, not theoretical.
2. Structs overlaid on `Task.data` (32 B) grow past it — e.g. `ListMenu` is 48 B
   on host because `ListMenuTemplate` holds three 8-B pointers. Measured, not
   inferred. `SaveBlock1` similarly overflows its flash sectors (15952 > 15872).

Precedent (`SAT-R/sa2`, a full GBA decomp) runs 64-bit native by making the
source pointer-width-agnostic (`uintptr_t`, `sizeof(void*)`-scaled storage),
*not* by building 32-bit. macOS-arm64 has no 32-bit userland, so ILP32 would
mean emulated x86 on Linux — rejected. wasm32 (ILP32) was also rejected: it
abandons the Go+cgo native-code model.

Measured refactor surface on pokeemerald: ~31 `Set/GetWordTaskArg`, 15
`Store/LoadWordFromTwoHalfwords`, 5 `sprite->data` casts, ~83 raw `(u32)`
pointer casts, 12 `STATIC_ASSERT` size checks — concentrated behind a handful of
accessor macros, not bespoke per-site edits.

## Shim design (next block)
Open sub-decisions to settle during implementation:
- **Overlay capacity:** widen the physical bytes behind `Task.data`/`Sprite.data`
  (host-only union) so oversized overlays fit without corruption, while keeping
  `s16 data[i]` index semantics for existing value slots.
- **STATIC_ASSERT:** the 12 size guards measure `sizeof(->data)` and legitimately
  differ on host; make them host-aware rather than blanket-disabled (the
  SaveBlock guard still matters for save-format design).
- **Packed pointers:** the `(u32)ptr` sites lose bits at the cast. Evaluate a
  low-4 GB mmap arena (makes in-arena pointers 32-bit-lossless, fixing most
  sites unmodified) vs. pointer-aware helpers per site. Note the arena does not
  cover code/static pointers (macOS text is > 4 GB).
- **Save:** serialize `SaveBlock*` to the exact 128 KB flash layout rather than
  `memcpy`-ing the LP64 struct; keep round-trip with real hardware / the oracle.

## Link surface (from linking the 306 host objects + an AgbMain entry)
2552 undefined symbols, three buckets:
- **Data layer — 2353 (92%)**: text, event/battle/movement scripts, palettes,
  gfx, tilemaps, maps. From `data/*.s` + generated data, never assembled for
  host yet. Carries the LP64 landmine: script macros emit `.4byte SYMBOL`
  pointers; a 4-byte reloc to a >4 GB host address overflows at link.
  Strategy: shadow the asm pointer macros to emit `.quad` on host and make the
  matching C interpreters read pointer-sized entries (the C-side `uintptr_t`
  shim, applied to data). Pure-data (text/gfx/incbin) assembles as-is.
- **BIOS SWIs / math — ~18**: DONE in `port/hle/bios.c` (CpuSet/CpuFastSet,
  LZ77/RL decompress, Sqrt/Div/ArcTan2 via libm, affine via libm trig — affine
  exactness TODO vs oracle; RegisterRamReset/SoftReset/VBlankIntrWait/MultiBoot
  stubbed).
- **Sound + boot/multiboot/IRQ — ~180**: `m4a*`, `MultiBoot*`, `IntrMain`,
  `IntrSIO32`. Stub for Phase 0.

## Data layer: convert data/*.s -> C (decided)
~21K lines of macro GNU-as bytecode (battle/anim/AI scripts, maps, text) can't be
host-assembled: clang's integrated assembler rejects pokeemerald's `.macro`
syntax, and GNU-as on macOS yields ELF that won't link into mach-o. Chosen path
is sa2's: data lives in C so the compiler emits native-width pointers.

Converter design (input validated): assemble each `.s` with `arm-none-eabi-as`
(handles the macros) to elf32, then read (section bytes) + (symbol offsets) +
(R_ARM_ABS32 relocations: offset -> target symbol). Emit native-pointer C:
- pure pointer tables (jump tables) -> `const u8 *const tbl[] = { &sym, ... }`.
- bytecode bodies -> packed struct interleaving byte-runs with native-width
  pointer members at the reloc offsets, so pointers grow 4->8 B correctly.
Open sub-problems for the converter build: interior labels (labels at arbitrary
offsets inside a blob — C can't place an extern symbol mid-struct, so each label
likely emits as its own object) and fall-through between adjacent labels (needs
opcode-terminator knowledge). Coupled with the C-side script-interpreter shim so
pointer reads/advances are pointer-width (the `uintptr_t` shim applied to the
script engine).

Validation done: `data/field_effect_scripts.s` assembles clean; relocations are a
tidy offset->symbol table. Next block = build the converter, prove one file
end-to-end (emit C -> compile -> link -> symbol resolves), then scale to 15 files.

## Phase status (autonomous run)

Build + run: `./port/build.sh && ./port/build/pe_native` (writes frame PPMs to
/tmp). Single binary, native arm64 machine code, no emulation.

- **Phase 0 — boot: DONE.** AgbMain runs the full init + main loop natively.
  GBA memory mapped at fixed high base 0x400000000; address macros rebased by a
  compile-time constant (`port/include/gba/{io_reg,defines}.h`). WaitForVBlank
  patched (`PORT_HOST`) to drive `VBlankIntr` + `port_frame_end` cooperatively —
  single-threaded, race-free, and the shape the cgo per-frame path will reuse.
- **Phase 1 — render: DONE.** `port/hle/ppu.c` renders mode-0 tiled BGs
  (4/8bpp, scroll, flip, priority) + non-affine sprites. Verified: copyright
  screen and intro scene render recognizably.
- **Phase 2 — input: DONE (functionally).** KEYINPUT injected from
  `port_frame_end` (active-low; baseline 0x3FF). Scripted A-taps drive
  title -> menu -> intro; game-state (callback2) transitions confirm it.
- **Phase 3 — full PPU: PARTIAL.** Affine BGs (mode 1/2) added; title screen
  renders cleanly. MISSING: windows (WIN0/1/OBJ), blend (BLDCNT/BLDALPHA/BLDY),
  mosaic, affine sprites, per-scanline effects (HBlank/scanline_effect).
  The title-background artifacts turned out **not** to be a PPU gap: (1) the
  host had no DMA controller, so `DmaFill`/`DmaCopy` (bare register writes) were
  no-ops — VRAM/palette went uncleared and `TransferPlttBuffer` never ran; now
  HLE'd in `port/hle/dma.c` via a shadowed `DmaSetUnchecked`. (2) upstream
  `global.h` stubs `INCBIN` to `{0}` under `__APPLE__`, which silently zeroed
  every non-`INCGFX` asset (e.g. `gTitleScreenBgPalettes`); the shadow `global.h`
  now un-stubs it so `preproc` emits the real bytes.
- **Phase 4 — audio: NOT STARTED.** m4a engine + song data are stubbed
  (silent). Needs a host MP2K mixer driven once/frame (see agbplay/ipatix), or
  a portable m4a.c reimplementation, plus converting `sound/songs/*.s` to C
  (the dataconv path already handles the format).
- **Phase 5 — save: NOT STARTED.** Flash stubbed; `gFlashMemoryPresent` forced
  absent. Needs a host Flash-1M (128 KB) HLE backing a `.sav`, field-wise
  SaveBlock serialization (the neutered STATIC_ASSERT constraint moves here),
  and round-trip vs mGBA/the oracle.

## Skipped / stubbed — for review
- **Pointer-width shim NOT applied.** The ~130 `SetWordTaskArg`/`(u32)ptr`
  sites (docs above) are unfixed; a pointer packed into `Task.data`/`Sprite.data`
  truncates on LP64. Menus/battles that store pointers there will misbehave or
  crash. This is the largest correctness debt. Boot/title happen not to hit a
  fatal case, but overworld/battle will.
- **Stubbed subsystems** (`port/build.sh` removes their objects; genstubs
  no-ops the symbols): librfu/link_rfu (wireless — polled forever), siirtc/rtc
  (GPIO at raw 0x080000C4, un-rebased), agb_flash* (self-modifying IWRAM
  routine). RTC time is zero; save is absent; link is dead.
- **`port_frame_end` re-arms the boot callback** because flash reads absent
  nulls callback2. With a real flash HLE (Phase 5) this hack goes away.
- **Sound song data + m4a**: 547 data + 210 fn symbols stubbed to empty/no-op.
- **STATIC_ASSERT neutered on host** (`port/include/global.h`): size guards
  don't hold under LP64; save-size guard must return in Phase 5 serialization.
- **PPU exactness**: affine uses libm-free integer math but reference-point /
  wrap handling is first-pass; BIOS affine (bios.c) uses libm trig, not the
  exact BIOS sine table. Diff against the emulator oracle when tightening.

## Reference
- `SAT-R/sa2` — full GBA decomp, 64-bit-native via `uintptr_t` shim.
- `toucans/pokeemerald_SDL3` — pokeemerald-derived, native macOS + wasm32;
  useful for an SDL3 platform layer and m4a-on-SDL audio.

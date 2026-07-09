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

### 2026-07-09 — reaches the overworld; New Game intro playable end-to-end
Drove the game (headless `cmd/emerald-drive`, scripted input) from title through
the full New Game intro into Littleroot and inside Brendan's house; the overworld
BG, sprites, dialog, warps, and door animations all render. Bugs fixed this pass
(all as `port/patches/` or `port/tools/dataconv`, never subtree edits):
- **dataconv struct layout**: the converter packed pointer slots with no host
  alignment padding and left NULL pointers 4 bytes wide, corrupting every
  fixed-layout map struct. Added a schema-driven mode (MapHeader / MapEvents /
  MapConnections / MapLayout / ObjectEventTemplate / Coord/BgEvent) that
  reproduces the exact LP64 layout. Map headers are found structurally as
  `gMapGroup_*` reloc targets. Also fixed a swapped `_MapConnections` vs
  `_MapConnectionsList` schema mapping.
- **script pointer width**: field-script operands are pointer-width (8 B) after
  conversion but the interpreter read 4. Added `ScriptReadPtr` (adaptive: a zero
  low word is a 4-byte NULL, else a full pointer — the converter makes the stream
  self-describing), widened `ScriptContext.data` to `uintptr_t` (shadow
  `script.h`), fixed `T*_READ_PTR` (shadow `global.h`) + map-script table strides,
  and the field-effect script interpreter (`FieldEffectScript_ReadPtr`).
- **door animation**: packed a 64-bit gfx/frame pointer into 16-bit task halves;
  stash the full pointer across free `data[]` slots (like `pokemon_animation`).
- **tile-data heap buffer**: `DecompressAndLoadBgGfxUsingHeap` stored the decompressed
  buffer via `SetWordTaskArg(…, (u32)ptr)` (32-bit) and the freer `Free`d the
  truncated pointer — crashed on returning from any heap-gfx screen (e.g. the wall
  clock) and on soft-reset-from-overworld. Stash/read the full pointer across
  `data[2..5]`. (Generic loader, so this covers many menus/screens.)
- **followup TaskFunc** (`SetTaskFuncWithFollowupFunc`/`SwitchTaskToFollowupFunc`):
  the generic mechanism packed a TaskFunc into two `s16` data slots → truncated on
  LP64, so `RunTasks` called a bad address. Crashed opening the field START menu
  (and any task using a followup func). Fixed with a full-width side table keyed by
  taskId (`port/patches/src/task.c.patch`).

### 2026-07-09 — first battle playable end-to-end (rescue vs wild Zigzagoon)

Two keystone fixes got the battle engine from "renders then crashes" to a full
tutorial battle (pick Mudkip → win → Birch rescued):

- **`port/hostcc.sh` was silently stubbing patched files** — the single highest-
  impact bug of the session. A *patched* source compiles from a temp scratch dir,
  so its `#include "data/foo.h"` (relative to `src/`) missed `src/data/` and the
  whole translation unit failed → every function in it got genstubbed to garbage.
  `battle_anim.c` was the victim: `IsContest()` returned a register-leftover (68,
  not 0/1) so `GetBattlerSpriteCoord` took the contest branch and dereferenced the
  NULL `gContestResources`. Fix: add `-iquote src` so patched and in-place builds
  resolve data headers identically. This un-stubbed ~15 functions + ~60 data
  symbols at once. Guard: `sweep.log`'s `FAIL src/...` lines flag any TU that stops
  compiling under a patch; watch the stub count (baseline ~196 fns).
- **battle-script NULL pointers weren't expanded 4→8 (`port/tools/dataconv`)**.
  dataconv grows a 4-byte GBA pointer to 8 bytes only where the ELF carries a
  relocation; a `.4byte NULL` operand (e.g. the common `tryfaintmon battler`
  macro) has none, so it stayed 4 bytes while the fixed-stride interpreter read 8
  → every instruction after a NULL operand desynced → wild-pointer `SIGBUS`. Fix:
  dataconv now parses `asm/macros/battle_script.inc` + the opcode enum into a
  per-opcode operand layout and walks each `BattleScript_*` body, expanding *every*
  pointer operand (NULL or relocated). Pointer-vs-value `.4byte` is decided by
  macro param name (`\ptr`, `*Ptr`, `src`, `dest`) with reloc-wins as backstop.
  Safe because all 36 section-relative script relocs point exactly at symbols
  (addend 0), so expansion doesn't perturb the `resolve()` offset math. Same NULL
  gap still latent in the AI (`gAIScriptPtr`) and anim (`sBattleAnimScriptPtr`)
  bytecode — apply the same layout-walk to `battle_ai_scripts`/anim when hit.
- **`accuracycheck` / `jumpifaffectedbyprotect` instruction lengths**: the
  automated offset transform only rewrote inline `gBattlescriptCurrInstr += N`
  and `T*_READ` offsets, so it missed instruction lengths passed as *arguments* to
  the `JumpIfMoveFailed(adder, …)` helper. Bumped the GBA literals to host lengths
  (5→9, 7→11). Any other helper that takes an instruction-length arg is the same
  class of bug.
- **`StoreSpriteCallbackInData6`/`SetCallbackToStoredInData6`**: a callback is a
  64-bit pointer, too wide for the two s16 `data[6]/[7]` slots; kept full-width in
  a `MAX_SPRITES` side table keyed by sprite index.

### 2026-07-09 — proactive static sweep of the pointer-truncation + bytecode classes

Rather than wait for each to crash, swept the whole tree for the two dominant
LP64 classes and fixed them cold. All via `port/patches/` + the `task.h` shadow.

- **Class 1/2 — code/host pointer packed into ≤32-bit task/sprite/word storage.**
  Grep signature: `(u32)` applied to a function pointer, or `>>16/<<16`
  reconstruction of a callback, or `SetWordTaskArg((u32)ptr)`. Fixes:
  - `task.h` (new shadow) + `task.c`: `SetWordTaskArg`/`GetWordTaskArg` widened to
    `uintptr_t` with a coherence-checked side table (`sWordArgs`) — still mirrors
    the low 32 bits into the slots and prefers the side table only when they match,
    so 32-bit value users that poke the slots directly still win. Fixes every
    `SetWordTaskArg`-pointer caller at once (easy_chat, pokemon_jump, pokenav,
    menu, pokenav_menu_handler_gfx). Pack sites also de-`(u32)`-cast.
  - Field-move callbacks (`fldeff_cut/dig/strength/rocksmash/teleport/misc/
    sweetscent`, `braille_puzzles`): the shared `data[8]/[9]` callback (Cut, Rock
    Smash, Strength, …) widened to a 4-slot store `data[8..11]` + matching read in
    `Task_DoFieldMove_RunFunc`. This one is **mid-game** (HMs).
  - `battle_factory_screen.c`: 12 `tFollowUpTaskPtrHi/Lo` pack sites + 5 unpacks →
    a `sSwapFollowUpTask[NUM_TASKS]` side table (Battle Frontier, post-game).
  - Already-fixed earlier: `shop.c`, `task.c` followup, `battle_anim_mons.c` sprite
    callbacks, `menu.c` (data[2..5]).
- **Class 3 — AI + anim bytecode interpreters.** Audited both cold by diffing the
  assembly macro operand layout against each handler's `+= N` strides:
  - `battle_ai_script_commands.c`: 7 commands (`if_has_move`, `if_level_cond`, …)
    were **over-strided** — the earlier auto-transform counted repeated
    `T1_READ_PTR` reads across branches as multiple pointers. Corrected to
    `gba + 4×(distinct pointers)`. AI needs no dataconv change: its jump targets
    are always relocated (generic path expands them) and its `.4byte` status words
    are read as values (`T1_READ_32`, left 4 bytes) — verified 0 stride mismatches.
  - `battle_anim.c`: already correct — it uses the robust `+= sizeof(void *)` idiom
    for pointer strides and `++`/`+= N` for value ops (audit's 10 "mismatches" were
    false positives from the leading opcode `++`). No change needed.
  - `contest_ai.c` (contests, optional): was fully unpatched (`T1_READ_PTR` reads 8
    bytes but strided `+= 5`). Every command has exactly one pointer (jump target,
    last) with no operand after it (verified by a safety scan → 0 flags), so added
    `+4` to all 98 pointer-command strides.

Method note: the dataconv NULL-pointer expansion (below) is only needed for
battle_scripts, where pointer operands are sometimes NULL. AI/anim/contest jump
targets are never NULL, so the generic reloc-expansion path + fixed interpreter
strides suffice.

### 2026-07-09 — trainer defeat text crash (32-bit-loaded pointer args)

With the AI guard letting the battle proceed, winning a trainer battle crashed in
`StringExpandPlaceholders` via `GetTrainerALoseText` — reading `sTrainerADefeatSpeech`
at a truncated address (fault `0x6283f19`, a host string pointer's low 32 bits).
`TrainerBattleLoadArgs` parses the inline `trainerbattle` script args and loaded
the intro/defeat-speech and script-return pointers with
`TRAINER_PARAM_LOAD_VAL_32BIT` → `SetU32(varPtr, T1_READ_32(data)); data += 4`.
On host those operands are 8-byte pointers (dataconv expanded the relocated
`.4byte`), so the read **truncated the pointer** *and* `data += 4` desynced every
later arg — including `LOAD_SCRIPT_RET_ADDR`, so the field script also resumed 8
bytes short of where it should per pointer. Every `_32BIT` param in every battle
table targets a pointer var (`u8 *`), so the fix is unconditional
(`battle_setup.c.patch`): `LOAD_VAL_32BIT` now does `SetPtr(varPtr, T2_READ_PTR(data));
data += sizeof(void *)`, and `CLEAR_VAL_32BIT` does `SetPtr(varPtr, NULL)` (the old
`SetU32(...,0)` zeroed only the low word, leaving a stale non-NULL high half).

The AI wild-pointer from the prior entry is still only guarded, not fixed — the
ring dump (`/tmp/ai_ring_dump.txt`) shows `AI_CheckBadMove` (logic 0) executing
`if_target_is_ally` (0x5e, correct +9 fall-through) then walking a run of `0x00`
bytes from offset +9 onward, where the static converted data has `0x19` — i.e. the
script bytes are zero at runtime past the first instruction. Points at the AI data
being partially overwritten at runtime (or its table entry pointing off-target),
not a stride bug. Next: dump `&AI_CheckBadMove[9]` at battle start vs the constructor.

### 2026-07-09 — trainer-battle AI crash (stride fix + a guard for the rest)

Trainer battles crash in `BattleAI_DoAIProcessing` — the dispatch `ldrb` reads
`*gAIScriptPtr` where `gAIScriptPtr` is a wild, unmapped address, while the
opponent chooses its first move. Two things came out of this:

**Real bug found (`+= 6` → `+= 10`).** `Cmd_if_equal_` (0x26) and
`Cmd_if_not_equal_` (0x27) read their jump target pointer-width
(`T1_READ_PTR(+2)`, 8 B) but still advanced by the **GBA fall-through length**
`gAIScriptPtr += 6` (operands are `byte param + 8-byte ptr` = 10 on host). On the
not-equal branch they landed 4 bytes short. These were the *only* survivors of the
earlier AI stride sweep: my audit extracted bodies with a `^static void NAME(void)$`
anchor, and both carry a trailing `// Same as if_equal.` comment, so the anchor
skipped them. Lesson: a source-scanning audit must not assume a canonical
signature line — a trailing comment hides a function.

**But that did not fix the crash.** After `+= 10`, a full CFG walk of the AI
bytecode from every `gBattleAI_ScriptsTable` entry (525 code labels, `if_in_*`
list-pointers correctly treated as data, not code) finds **0** remaining
inconsistencies: every pointer the interpreter reads has a matching relocation and
no value-read position is relocated. The interpreter is also self-consistent
(reads never overlap, strides equal operand-ends). The AI stack is
`const u8 *ptr[8]` (not truncated), and the table index can't overrun 32 entries.
So the wild `gAIScriptPtr` is **runtime memory corruption from outside the
interpreter**, not a bytecode/stride defect — root cause still open (candidate:
something in the send-out path just before the AI runs; the crash always follows
`Task_HandleMonAnimation`). Reproduction is blocked on getting a driver-scripted
player to a trainer (the save is on Route 103 post-rival; Oldale→Route102 blind
nav kept failing).

**Guard shipped as the stopgap** (`port/hle/aitrace.c` + `pe_ai_guard` in the
dispatch patch): before the interpreter dereferences `gAIScriptPtr`, probe its
readability; if bad, dump the last 24 opcodes (the corrupting transition) and have
the dispatch set `AI_ACTION_DONE` instead of faulting — so trainer battles degrade
(AI may skip a move) rather than crash, and the next battle logs the diagnosis to
`/tmp/emerald_trace.log`. `PE_AI=1` also enables a per-opcode trace. Remove once
the corruption source is found.

Probing readability is subtle on macOS: `mincore()` reports PROT_NONE/guard pages
as "mapped" (the first guard build *crashed inside the guard* on `*ptr`), and
`write(/dev/null, ptr, 1)` returns 1 for a wild pointer because /dev/null discards
without copying the buffer. The working probe is `write()` into a **pipe** (the
kernel copies → validates the byte → EFAULT on a bad pointer), draining the byte
back out on success. Unit-tested: valid→1, `0x102aa4564a8`→0, NULL→0.

Fault-pointer analysis (from an ASLR-corrected backtrace): the wild `gAIScriptPtr`
is not near the AI data (~`0x102a8xxxx` at runtime) — it is ~`0x102aa4564a8`, a
value whose bytes look **byte-shifted**, i.e. an 8-byte read straddling operands
(a misaligned read off by a non-multiple-of-4), not a clean stride/NULL desync.
The CFG walk rules out a bytecode cause, so this points at a runtime write landing
in the AI data or a mis-typed read in one handler; the ring dump will name the
exact command. Reproduction remains blocked (Route-102 approach reaches a
dialog-locked NPC the driver script can't get around).

### 2026-07-09 — list menus corrupt adjacent tasks (struct > task data under LP64)

Opening any list menu (the crash came via the Player's-PC ITEM STORAGE, but it's
every list menu: bag, shops, mail, move relearner, decorations, …) eventually
crashed by calling a corrupted task `func` — a valid address with `0x0001` OR'd
into bits 48-63. Root cause: `list_menu.c` stores whole structs *inline* in a
task's 32-byte `data[]` (`struct ListMenu *l = (void *)gTasks[t].data;`). On the
GBA those fit; under LP64 the pointer fields (`ListMenuTemplate.items` /
`moveCursorFunc` / `itemPrintFunc`, and the scroll/cursor structs' sprite/subsprite
pointers) push `sizeof` past 32 bytes, so the struct **overflows into the next
task's `func`**. The `STATIC_ASSERT(sizeof(struct ListMenu) <= sizeof(data))` that
would catch this is neutered on host (LP64 size guards don't hold). Fix
(`list_menu.c.patch`): keep the four structs (`ListMenu`, `ScrollIndicatorPair`,
`RedOutlineCursor`, `RedArrowCursor`) full-width in a `union sTaskStructs[NUM_TASKS]`
side table keyed by taskId, redirecting all 20 `(void *)gTasks[t].data` casts —
safe because these tasks never touch `data[]` directly (the `tState`=`data[0]`
defines are on *sprites*). A/B-verified: without it the bag list menu crashes,
with it it works. **Same latent pattern still in** `mystery_gift_menu.c`,
`wireless_communication_status_screen.c`, `union_room.c` (link-only, not yet
fixed). General watch item: any struct stored inline in task/sprite data guarded
by a now-neutered `STATIC_ASSERT` is an LP64 overflow.

### 2026-07-09 — save freezes after "saved the game" (IsSEPlaying stuck TRUE)

The post-save dialog hung on the success message (data written fine, but the START
menu never closed). `SaveReturnSuccessCallback` loops in `SAVE_IN_PROGRESS` until
`!IsSEPlaying()`, but `PlaySE(SE_SAVE)` sets the SE's `MUSICPLAYER_STATUS_TRACK`
bit and m4a's stubbed `m4aSoundMain` never clears it, so `IsSEPlaying()` is TRUE
forever. Fix (`sound.c.patch`): `IsSEPlaying()` returns `FALSE` on host — no real
audio plays, so nothing is ever "playing", and any wait-for-SE-to-finish proceeds
immediately (correct for the HLE). This unblocks any m4a-status busy-wait, not
just saving.

### 2026-07-09 — PC box-close crash (per-frame work on freed sStorage)

Closing the PC box crashed: `Task_ChangeScreen` runs `FreePokeStorageData()` (→
`sStorage = NULL`) then switches the callback, but the *same frame's*
`CB2_PokeStorage` continues its per-frame work on the now-NULL `sStorage` —
first `UpdateCloseBoxButtonFlash` (fault `0x32f`), then after that was guarded,
`AnimateSprites` → `SpriteCB_CursorShadow` (fault `0xe10`). Rather than chase each
callback, the fix guards the teardown frame's execution points: `CB2_PokeStorage`
returns right after `RunTasks()` if `sStorage == NULL` (covers the per-frame tail
including `AnimateSprites`' sprite callbacks), and `VBlankCB_PokeStorage`
(fault `0x334`) — the VBlank fires once more after the free, a *separate* path
from the CB2 loop — guards its `sStorage->bg2_X` read. Same GBA-benign-NULL class
as the cursor fix below. (Couldn't script the CLOSE-BOX button to verify the live
teardown — blind PC navigation, like the town door — but each guard sits on the
exact NULL deref the traces reported.)

### 2026-07-09 — PC cursor NULL-sprite write (GBA-benign, host-fatal)

Moving the PC storage cursor up to the buttons/party area crashed at fault `0x4`
in `SetMovingMonPriority` (`sStorage->movingMonSprite->oam.priority = ...` with a
NULL `movingMonSprite` when no mon is grabbed). `DoCursorNewPosUpdate` calls it
unconditionally for `CURSOR_AREA_BUTTONS`/`IN_PARTY`. On the GBA a write through a
NULL pointer just hits the BIOS region and is silently dropped (no MMU); on the
host it's a segfault. Fix (`pokemon_storage_system.c.patch`): null-guard the
write. This is a distinct class — "GBA-benign NULL write" — likely to recur; fix
each site with a null-guard as hit (a blanket low-page mmap to mimic the GBA was
considered and rejected: it would mask real NULL bugs and MAP_FIXED at 0 is
unreliable/hardened-off).

### 2026-07-09 — shadow headers silently bypassed on transitive include (PC crash)

Opening the PC / Pokémon Storage crashed in `DrawTextWindowAndBufferTiles` →
`CpuSet` on a 32-bit-truncated `tileData1` (`GetWindowAttribute(WINDOW_TILE_DATA)`
returned a heap pointer truncated to its low 32 bits). Root cause was **not** the
storage code: `include/text_window.h` does `#include "window.h"`, and a quoted
include resolves against the *including* header's own directory (`include/`)
before `-iquote ../port/include`, so the shadow `window.h` (which widens
`GetWindowAttribute` to `uintptr_t`) was bypassed for every TU that pulled it in
transitively. `window.h` (3 includers) and the new `task.h` shadow (6 includers)
were both affected — so the Class 2 `SetWordTaskArg` widening was itself partly
ineffective until this fix.

Fix: `port/mkinc.sh` stages `port/build/inc/` = a copy of `pokeemerald/include`
with the **self-contained** shadows (`window.h`, `task.h`, `malloc.h`, `script.h`)
overlaid *into* it, and the build compiles with `-I port/build/inc` so those
shadows win even transitively. Augmenter shadows (`global.h`, `gba/*.h`, which
`#include` the original) can't overlay cleanly (they'd shadow their own target),
so they stay on the `-iquote port/include` path — safe because they're always
included directly and first by each `.c` (include guards no-op the transitive
copies). `build.sh`/`hostcc.sh` INCS updated accordingly; verified both shadows
now resolve to `uintptr_t` in transitively-including TUs and the PC opens cleanly.

### 2026-07-09 — Poké Mart buy/sell crash (callback packed into 16-bit task slots)

Opening a shop's buy or sell menu crashed: `CallCallbacks` jumped to a garbage
pointer (`cb2=?` in the trace). `shop.c` packs `CB2_InitBuyMenu`/`CB2_GoToSellMenu`
into two s16 task slots (`tCallbackHi=data[8]`, `tCallbackLo=data[9]`) and rebuilds
a 32-bit pointer — truncating the 64-bit host callback. Fix (`shop.c.patch`): hold
the callback full-width in a file-scope `static MainCallback` (only one buy/sell
transition runs at a time), same idiom as the sprite-callback side table.

This is a recurring class (a code pointer stuffed into ≤32 bits of task/sprite
data). Already fixed: `task.c` `SetTaskFuncWithFollowupFunc` (side table),
`battle_anim_mons.c` `Store/SetCallbackToStoredInData6`, and now `shop.c`. **Still
latent** (post-game, not yet hit): `apprentice.c:1304` and `braille_puzzles.c:270/275`
(Regi HM callbacks) use the same `(u32)func >> 16` packing — fix them the same way
when the Battle Frontier / Regi puzzles are reached. `battle_anim_utility_funcs.c`'s
`selectedPalettes >> 16` is a real 32-bit *value*, not a pointer — leave it.

### 2026-07-09 — wild-battle transition crash (`VBlankCB_Slice` NULL-deref)

Walking into a wild encounter (any of the scanline-effect transitions: Slice,
Blur, Swirl, Wave, GridSquares, ShredSplit, …) crashed with `SIGSEGV` at fault
`0x2` inside `VBlankCB_Slice` (`VBlankIntr` → the transition's VBlank callback),
`sTransitionData->WININ` on a NULL `sTransitionData`. Cause: every transition's
`*_End` destroys its task but leaves its custom VBlank callback installed;
`IsBattleTransitionDone` then `FREE_AND_SET_NULL(sTransitionData)`. On real
hardware the battle init installs its own VBlank callback before the next VBlank
fires, but the port's frame model lets one more VBlank run with the stale
callback dereferencing the freed struct. Fix (`battle_transition.c.patch`): call
`SetVBlankCallback(NULL)` right before the free in `IsBattleTransitionDone` —
one place covers all transition types. Note: this only bites transitions that set
a scanline VBlank callback; the scripted first-battle (`B_TRANSITION_INTRO`, plain
fade) never hit it, which is why the rescue battle worked but the first wild
encounter didn't. (Also note the earlier red herring: this same crash was
initially misread as a boot/display bug because it only surfaced after ~12k frames
of actual play — PE_TRACE localised it to the transition frame.)

### 2026-07-09 — battery `.sav` (checkpoint support)
`port/hle/flash.c`'s flat 128 KB flash buffer is now optionally file-backed: set
`PE_SAV=path` and the buffer loads at boot (`pe_flash_init`, before the game reads
flash) and flushes when dirty once per frame (`pe_flash_flush` from
`port_frame_end`). So a save written in one process (GUI or headless driver) is a
checkpoint another can CONTINUE from. Verified end-to-end: START→SAVE writes the
`.sav`; booting with it → dismiss the battery-dry notice → CONTINUE lands back at
the saved tile. Notes: the "SAVING…" message waits for an A press on the port
(text-printer timing, harmless); RTC-stub battery-dry notice shows each boot; the
LP64 `SaveBlock1` (15952 B) can overflow the PC-storage sector — fine for
early-game checkpoints (empty boxes), the real field-wise-serialization fix is
still Phase-5 debt.
- **DMA to VRAM**: the GBA `DmaSet` register path is inert on host, so
  `RequestDma3Copy`/`Fill` never actually moved tilemaps/tiles to VRAM (overworld
  BG was all black). Host path now `memcpy`s/fills immediately; this also removed
  a map-load hang (the DMA3 queue was drained only by an async VBlank that never
  fired during `DoMapLoadLoop`'s spin).

Known remaining: a persistent green number renders top-left every frame (debug
overlay, source unidentified); RTC still stubbed (clock-set event untested).

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
  now un-stubs it so `preproc` emits the real bytes. The same `__APPLE__` block
  stubs the charmap macros `_()`/`__()` to `{x}`, so `clang -E` expanded them
  before `preproc` could charmap-encode the literal and every string compiled to
  raw ASCII (all dialog text rendered as garbage glyphs); the shadow `global.h`
  un-stubs those too.
- **Phase 4 — audio: NOT STARTED.** m4a engine + song data are stubbed
  (silent). Needs a host MP2K mixer driven once/frame (see agbplay/ipatix), or
  a portable m4a.c reimplementation, plus converting `sound/songs/*.s` to C
  (the dataconv path already handles the format).
- **Phase 5 — save: PARTIAL.** `port/hle/flash.c` provides a Flash-1M (128 KB)
  HLE: a flat sector buffer, `IdentifyFlash`/`ReadFlash`/`ProgramFlashByte`/
  `EraseFlashSector`/`ProgramFlashSectorAndVerify` implemented, so
  `gFlashMemoryPresent` is now TRUE and the main menu reaches NEW GAME instead of
  the `SAVE_STATUS_NO_FLASH` error dialog. Still MISSING: the buffer is
  session-only (no `.sav` backing yet) and field-wise SaveBlock serialization
  (the neutered STATIC_ASSERT constraint moves here), plus round-trip vs
  mGBA/the oracle.

## Skipped / stubbed — for review
- **Pointer-width shim NOT applied.** The ~130 `SetWordTaskArg`/`(u32)ptr`
  sites (docs above) are unfixed; a pointer packed into `Task.data`/`Sprite.data`
  truncates on LP64. Menus/battles that store pointers there will misbehave or
  crash. This is the largest correctness debt. Boot/title happen not to hit a
  fatal case, but overworld/battle will.
- **Stubbed subsystems** (`port/build.sh` removes their objects; genstubs
  no-ops the symbols): librfu/link_rfu (wireless — polled forever), siirtc/rtc
  (GPIO at raw 0x080000C4, un-rebased). `agb_flash*` is also dropped, but its
  API is now reimplemented in `port/hle/flash.c` rather than no-op'd. RTC time is
  zero (so the main menu shows the "internal battery has run dry" notice once);
  link is dead.
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

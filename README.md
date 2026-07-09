# emerald

Running **Pokémon Emerald** as native host machine code, driven from Go.

> [!WARNING]
> **Status: early and experimental.** It boots and runs up through the title
> sequence and Prof. Birch's introduction, but is unstable beyond that.

![Pokémon Emerald's title screen, running as native code and rendered through Sapphire's PPU](docs/screenshot.png)

This project compiles the [pret/pokeemerald](https://github.com/pret/pokeemerald)
decompilation to native arm64 — not an emulator, the game's own C running
directly — and drives it from Go over a per-frame cgo boundary, rendering
through the [Sapphire](https://github.com/dbut2/sapphire) GBA emulator's PPU and
window. None of this is possible without pokeemerald; this repository is a fork
of it, and the decompilation is vendored as a git subtree under
[`./pokeemerald`](./pokeemerald).

## How it fits together

- `pokeemerald/` — the decomp, vendored as a subtree (the game logic + data).
- `port/` — the host port: shadow headers that redirect GBA hardware to host
  memory, a BIOS/PPU/memory HLE in C, and a converter that turns the decomp's
  assembly data into native-pointer C.
- `internal/core`, `cmd/emerald` — the Go bindings: a single Go binary that runs
  the native core and renders it through Sapphire.

## Build & run

```sh
cd pokeemerald && make modern && cd ..   # asset + header substrate (once)
./port/build.sh                          # compile the native core -> libpe.a
go build -o emerald ./cmd/emerald && ./emerald
```

Keys: **Z**/**X** = A/B, arrows, **Enter** = Start, **Backspace** = Select,
**A**/**S** = L/R, **Space** = fast-forward.

## Crash reporting

The game core is still unstable, so `emerald` supervises itself: when it dies,
it captures the C backtrace and opens a prefilled
[new issue](https://github.com/dbut2/emerald/issues/new) in your browser, filed
as whoever you're signed into GitHub as. Nothing is submitted until you press
**Submit** — read the log and add what you were doing first. The full log is
written to a temporary file and its path is printed.

Set `EMERALD_NO_CRASH_REPORT=1` to disable this and run unsupervised.

Reports are tagged `crash`, which triggers
[`.github/workflows/crash-autofix.yaml`](./.github/workflows/crash-autofix.yaml):
Claude reads the backtrace, and either opens a PR that closes the issue on merge
or comments explaining why it couldn't. GitHub only honours the `crash` tag for
users with triage rights, so an outside reporter's issue waits for a maintainer
to label it. CI can't build the native core on Linux, so those PRs are
compile-checked on the Go side only and are never merged unreviewed.

## Status

Early and experimental. It boots and renders the intro and title screen and runs
at native speed. Sound and save are stubbed, and a pointer-width shim for 64-bit
hosts is still needed before the overworld is stable — see
[`docs/native-port.md`](./docs/native-port.md) for the full design and status.

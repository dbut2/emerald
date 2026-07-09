#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
INC="$ROOT/port/build/inc"
rm -rf "$INC"; mkdir -p "$INC"
cp -R "$ROOT/pokeemerald/include/." "$INC/"
# Only the self-contained replacement shadows overlay cleanly; augmenter shadows
# (global.h, gba/*.h) #include the original and stay on the -iquote path instead.
for h in window.h task.h malloc.h script.h; do
  cp "$ROOT/port/include/$h" "$INC/$h"
done

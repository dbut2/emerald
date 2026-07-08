#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PE="$ROOT/pokeemerald"
src="$1"; out="$2"
cd "$PE"

INCS="-iquote ../port/include -I include -I ."
DEFS="-DMODERN=1 -DNDEBUG -DPORT_HOST=1"

# clang cc1 outputs assembly on the GBA path; here it compiles C directly, but
# INCBIN emits .incbin against paths relative to the pokeemerald root, so CWD
# and the integrated assembler must resolve from $PE.
clang -E $INCS $DEFS -Wno-trigraphs "$src" \
  | tools/preproc/preproc -i -g build/assets "$src" charmap.txt \
  | clang -c -std=gnu11 -x c - -o "$out" -Wno-everything

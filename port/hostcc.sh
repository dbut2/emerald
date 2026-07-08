#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PE="$ROOT/pokeemerald"
src="$1"; out="$2"
cd "$PE"

INCS="-iquote ../port/include -I include -I ."
DEFS="-DMODERN=1 -DNDEBUG -DPORT_HOST=1"

# Host-only fixes live as unified diffs under port/patches/, never edited into
# the subtree; compile a patched scratch copy (basename kept so #line/INCBIN resolve).
compile_src="$src"
patch_file="$ROOT/port/patches/$src.patch"
if [ -f "$patch_file" ]; then
  tmpd="$(mktemp -d)"; trap 'rm -rf "$tmpd"' EXIT
  compile_src="$tmpd/$(basename "$src")"
  cp "$src" "$compile_src"
  patch -s --fuzz=0 "$compile_src" < "$patch_file"
fi

# clang cc1 outputs assembly on the GBA path; here it compiles C directly, but
# INCBIN emits .incbin against paths relative to the pokeemerald root, so CWD
# and the integrated assembler must resolve from $PE.
clang -E $INCS $DEFS -Wno-trigraphs "$compile_src" \
  | tools/preproc/preproc -i -g build/assets "$src" charmap.txt \
  | clang -c -std=gnu11 -g -fno-omit-frame-pointer -x c - -o "$out" -Wno-everything

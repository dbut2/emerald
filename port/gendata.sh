#!/usr/bin/env bash
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PE="$ROOT/pokeemerald"
GEN="$ROOT/port/build/gen"; mkdir -p "$GEN"
OBJ="$ROOT/port/build/obj"; mkdir -p "$OBJ"
PRE="$PE/tools/preproc/preproc"
CONV="$ROOT/port/build/dataconv"

go build -o "$CONV" "$ROOT/port/tools/dataconv" || exit 1
cd "$PE"

asm_ok=0; conv_ok=0; cc_ok=0; fail=()
for src in data/*.s; do
  name=$(basename "$src" .s)
  o="$GEN/$name.elf.o"; c="$GEN/${name}_data.c"; obj="$OBJ/_data_${name}.o"
  if ! { $PRE "$src" charmap.txt 2>/dev/null \
        | clang -E -I include -I . -DMODERN=1 - 2>/dev/null \
        | $PRE -ie "$src" charmap.txt 2>/dev/null \
        | arm-none-eabi-as -mcpu=arm7tdmi --defsym MODERN=1 -o "$o" - 2>/dev/null; }; then
    fail+=("$name (assemble)"); continue
  fi
  asm_ok=$((asm_ok+1))
  if ! "$CONV" "$o" "$c" 2>/dev/null; then fail+=("$name (convert)"); continue; fi
  conv_ok=$((conv_ok+1))
  if ! clang -c -std=gnu11 -Wno-everything "$c" -o "$obj" 2>"$GEN/$name.cc.err"; then
    fail+=("$name (compile: $(grep -c error: "$GEN/$name.cc.err") errs)"); continue
  fi
  cc_ok=$((cc_ok+1))
done
echo "assembled: $asm_ok  converted: $conv_ok  compiled: $cc_ok  / $(ls data/*.s | wc -l | tr -d ' ')"
[ ${#fail[@]} -gt 0 ] && printf 'FAIL %s\n' "${fail[@]}"
exit 0

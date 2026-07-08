#!/usr/bin/env bash
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PE="$ROOT/pokeemerald"
OBJ="$ROOT/port/build/obj"; mkdir -p "$OBJ"
LOG="$ROOT/port/build/sweep.log"; : > "$LOG"

ok=0; fail=0; failed=()
while IFS= read -r src; do
  rel="${src#"$PE"/}"
  o="$OBJ/$(echo "$rel" | tr '/' '_' | sed 's/\.c$/.o/')"
  if "$ROOT/port/hostcc.sh" "$rel" "$o" >>"$LOG" 2>&1; then
    ok=$((ok+1))
  else
    fail=$((fail+1)); failed+=("$rel")
    echo "FAIL $rel" >>"$LOG"
  fi
done < <(find "$PE/src" -name '*.c' | sort)

echo "compiled OK: $ok   failed: $fail   total: $((ok+fail))"
printf '  %s\n' "${failed[@]:0:40}"

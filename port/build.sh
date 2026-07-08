#!/usr/bin/env bash
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PE="$ROOT/pokeemerald"
OBJ="$ROOT/port/build/obj"
INCS="-iquote $ROOT/port/include -I $PE/include -I $PE"
DEFS="-DMODERN=1 -DNDEBUG -DPORT_HOST=1"

echo "[1/5] compiling src/*.c"
"$ROOT/port/sweep.sh" >/dev/null

echo "[2/5] compiling data/*.s"
"$ROOT/port/gendata.sh" >/dev/null

echo "[3/5] compiling HLE (bios, mem, bridge)"
for f in bios mem dma m4a flash; do
  clang -c -std=gnu11 $INCS $DEFS -Wno-everything "$ROOT/port/hle/$f.c" -o "$OBJ/_hle_$f.o" \
    || { echo "HLE $f.c failed to compile"; exit 1; }
done
clang -c -std=gnu11 $INCS $DEFS -Wno-everything "$ROOT/port/bridge.c" -o "$OBJ/_bridge.o" \
  || { echo "bridge.c failed to compile"; exit 1; }
printf 'extern void AgbMain(void);\nint main(void){AgbMain();return 0;}\n' | clang -x c -c - -o "$ROOT/port/build/_main.o"

# Hardware subsystems stubbed for single-player boot: they poll/self-modify
# hardware that does not exist on host (see docs/native-port.md).
echo "[4/5] removing stubbed subsystems (rfu, rtc, flash)"
rm -f "$OBJ"/*librfu_rfu.o "$OBJ"/*librfu_stwi.o "$OBJ"/*librfu_sio32id.o "$OBJ"/*link_rfu*.o
rm -f "$OBJ"/*siirtc.o "$OBJ"/*_rtc.o "$OBJ"/*agb_flash*.o

echo "[5/5] stubbing remaining undefined + archiving libpe.a"
"$ROOT/port/genstubs.sh" >/dev/null
rm -f "$ROOT/port/build/libpe.a"
ar rcs "$ROOT/port/build/libpe.a" "$OBJ"/*.o
echo "built: port/build/libpe.a  (link with -Wl,-force_load)"

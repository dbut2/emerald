#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
./port/build.sh
go build -a -o ./emerald ./cmd/emerald
echo "launching — trace at /tmp/emerald_trace.log"
PE_SAV="$HOME/emerald.sav" PE_TRACE=1 ./emerald 2>&1 | tee /tmp/emerald_trace.log

#!/bin/bash
# Benchmark: espresso vs node vs bun
# Runs each runtime 5 times and reports avg ± stddev for time and peak memory
# Usage: ./bench/run.sh

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
BENCH="$SCRIPT_DIR/bench.js"
RUNS=5

echo "=== Espresso vs Node vs Bun Benchmark ==="
echo "Running $RUNS iterations each..."
echo ""

# Verify outputs match first
echo "--- Verifying correctness ---"
espresso "$BENCH" 2>&1
echo ""

# Parse "0m0.123s" into milliseconds
parse_time() {
  echo "$1" | awk -F'[ms]' '{printf "%.0f\n", $1*60000 + $2*1000}'
}

# Get peak RSS of a command in KB (Linux /proc-based)
get_peak_rss() {
  local cmd=$1
  local file=$2
  $cmd "$file" > /dev/null 2>&1 &
  local pid=$!
  local peak=0
  while kill -0 $pid 2>/dev/null; do
    local rss=$(awk '/VmHWM/{print $2}' /proc/$pid/status 2>/dev/null)
    if [ -n "$rss" ] && [ "$rss" -gt "$peak" ] 2>/dev/null; then
      peak=$rss
    fi
  done
  wait $pid 2>/dev/null
  echo $peak
}

run_bench() {
  local name=$1
  local cmd=$2
  local times=""
  local mems=""

  for i in $(seq 1 $RUNS); do
    raw=$( { time $cmd "$BENCH" > /dev/null 2>&1; } 2>&1 )
    t=$(echo "$raw" | grep '^real' | awk '{print $2}')
    ms=$(parse_time "$t")
    times="$times $ms"
  done

  # Get memory once (representative)
  local mem_kb=$(get_peak_rss "$cmd" "$BENCH")

  echo "$times" | awk -v name="$name" -v mem="$mem_kb" '{
    n = NF
    sum = 0; for(i=1;i<=n;i++) sum += $i
    avg = sum / n
    sq = 0; for(i=1;i<=n;i++) sq += ($i - avg)^2
    sd = sqrt(sq / n)
    mb = mem / 1024

    printf "  %-10s  %6.1f ± %4.1f ms  %5.1f MB   [", name, avg, sd, mb
    for(i=1;i<=n;i++) { if(i>1) printf ", "; printf "%d", $i }
    print "]"
  }'
}

run_bench "espresso" "espresso"

if command -v node &> /dev/null; then
  run_bench "node" "node"
fi

if command -v bun &> /dev/null; then
  run_bench "bun" "bun"
fi

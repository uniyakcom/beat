#!/usr/bin/env bash
#
# bench.sh — 运行完整基准测试并生成 benchmarks_*.txt 基线文件
#
# 用法:
#   ./scripts/bench.sh              # 默认 benchtime=3s, count=1
#   ./scripts/bench.sh 5s 3         # benchtime=5s, count=3
#   BENCH_SKIP_COMPARE=1 ./scripts/bench.sh  # 跳过竞品对比
#
set -euo pipefail

cd "$(dirname "$0")/.."

BENCHTIME="${1:-3s}"
COUNT="${2:-1}"

# ── 检测系统信息 ────────────────────────────────────────────────

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

HAS_LSCPU=false
if command -v lscpu &>/dev/null; then
  HAS_LSCPU=true
fi

case "$OS" in
  linux)
    if $HAS_LSCPU; then
      CORES=$(lscpu | awk '/^Core\(s\) per socket:/ {print $NF}')
    else
      # fallback: /proc/cpuinfo — 物理核心数 = cpu cores (per socket)
      CORES=$(awk '/^cpu cores/ {print $NF; exit}' /proc/cpuinfo 2>/dev/null)
      [[ -z "$CORES" || "$CORES" == "0" ]] && CORES=$(grep -c '^processor' /proc/cpuinfo 2>/dev/null || echo "?")
    fi
    THREADS=$(nproc 2>/dev/null || grep -c '^processor' /proc/cpuinfo 2>/dev/null || echo "?")
    VCPUS=$THREADS
    ;;
  darwin)
    CORES=$(sysctl -n hw.physicalcpu)
    THREADS=$(sysctl -n hw.logicalcpu)
    VCPUS=$THREADS
    ;;
  *)
    CORES=$(nproc 2>/dev/null || echo "?")
    THREADS=$(nproc 2>/dev/null || echo "?")
    VCPUS=$THREADS
    ;;
esac

OUTFILE="benchmarks_${OS}_${CORES}c${THREADS}t_${VCPUS}vc.txt"

echo "=== Beat Benchmark ==="
echo "  OS:       $OS/$ARCH"
echo "  Cores:    $CORES  Threads: $THREADS  vCPUs: $VCPUS"
echo "  Output:   $OUTFILE"
echo "  Options:  benchtime=$BENCHTIME  count=$COUNT"
echo ""

# ── 生成基线文件 ────────────────────────────────────────────────

{
  # 头部信息
  echo "# $(date '+%Y-%m-%d %H:%M:%S %Z')"
  echo "# $(uname -a)"
  echo ""

  # 系统信息
  case "$OS" in
    linux)
      if $HAS_LSCPU; then
        echo '$ lscpu'
        lscpu
      else
        echo '$ cat /proc/cpuinfo | head'
        head -30 /proc/cpuinfo
      fi
      ;;
    darwin)
      echo '$ sysctl machdep.cpu hw.physicalcpu hw.logicalcpu hw.memsize'
      sysctl machdep.cpu.brand_string hw.physicalcpu hw.logicalcpu hw.memsize hw.l1dcachesize hw.l2cachesize hw.l3cachesize 2>/dev/null || true
      ;;
  esac
  echo ""

  echo '$ go version'
  go version
  echo ""

  # 主基准测试
  BENCH_CMD="go test -bench=\".\" -benchmem -benchtime=${BENCHTIME} -count=${COUNT} -run=\"^\$\" ."
  echo "$ ${BENCH_CMD}"
  eval "$BENCH_CMD" 2>&1

  # 竞品对比
  if [[ -z "${BENCH_SKIP_COMPARE:-}" ]] && [[ -d "_benchmarks" ]]; then
    echo ""
    echo "# _benchmarks (vs)"
    COMPARE_CMD="go test -bench=\".\" -benchmem -benchtime=${BENCHTIME} -count=${COUNT} -run=\"^\$\" ./_benchmarks/"
    echo "$ ${COMPARE_CMD}"
    (cd _benchmarks && go test -bench="." -benchmem -benchtime="${BENCHTIME}" -count="${COUNT}" -run="^$" .) 2>&1
  fi

} | tee "$OUTFILE"

echo ""
echo "=== Done: $OUTFILE ==="

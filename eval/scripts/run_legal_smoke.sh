#!/usr/bin/env bash
# ── Legal Review Smoke Test ────────────────────────────────────
# Ported from FangLab live_smoke_business.sh pattern
# Tests the L4 legal_review_plugin + L3 plugin chain via HTTP API.
#
# Pre-requisites:
#   DEEPSEEK_API_KEY set (or .env file at project root)
#   go build ./... passed
#
# Usage:
#   bash eval/scripts/run_legal_smoke.sh [--api URL] [--strict]
#
# Exit: 0 = all pass, 1 = failures

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

# shellcheck source=../lib/lib.sh
source "$PROJECT_ROOT/eval/lib/lib.sh"

API_URL="${BEISHAN_API_URL:-http://127.0.0.1:8013}"
STRICT=false
APP_PORT=8013
APP_PID=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --strict) STRICT=true; shift ;;
    --api)    API_URL="$2"; shift 2 ;;
    *)        die "Unknown flag: $1" ;;
  esac
done

OUT_DIR="$OUT_DIR_BASE/legal_smoke_$(date +%Y%m%d_%H%M%S)"
mkdir -p "$OUT_DIR"

PASS_COUNT=0
FAIL_COUNT=0
TOTAL_COUNT=0

info "=== beishan-core Legal Smoke Test ==="
info "API: $API_URL"
info "Output: $OUT_DIR"
info ""

# ── Preflight: API availability ────────────────────────────────
info "[preflight] 检查 $API_URL ..."
if ! wait_for_service "$API_URL" 10; then
  info "[preflight] API 未运行，尝试本地启动..."

  if ! command -v go &>/dev/null; then
    die "go 未安装"
  fi

  cd "$PROJECT_ROOT"
  go build -o "$OUT_DIR/beishan-core" ./cmd/beishan/ 2>&1 | tee "$OUT_DIR/build.log"
  if [ ! -f "$OUT_DIR/beishan-core" ]; then
    die "编译失败，见 $OUT_DIR/build.log"
  fi

  # 后台启动服务
  "$OUT_DIR/beishan-core" &
  APP_PID=$!
  info "[preflight] 服务已启动 (PID $APP_PID)"

  if ! wait_for_service "$API_URL" 30; then
    die "服务启动超时"
  fi
  info "[preflight] API 就绪"
fi
info ""

# ── Test cases ──────────────────────────────────────────────────
run_test() {
  local id="$1"
  local prompt="$2"
  local expected_plugin="$3"
  local expected_type="$4"
  local recipient="$5"
  local timeout="${6:-30}"

  TOTAL_COUNT=$((TOTAL_COUNT + 1))
  local test_out="$OUT_DIR/$id"
  mkdir -p "$test_out"

  info "[test $TOTAL_COUNT] $id — $expected_type"

  local payload_json
  if [[ "$prompt" == \{* ]]; then
    payload_json="$prompt"
  else
    payload_json=$(echo "$prompt" | jq -Rs .)
  fi

  local response
  response=$(curl -s -X POST "$API_URL/api/chat" \
    --max-time "$timeout" \
    -H "Content-Type: application/json" \
    -d "{\"sender\":\"user\",\"recipient\":\"$recipient\",\"type\":\"$expected_type\",\"payload\":$payload_json}" 2>&1 || true)

  if [ -z "$response" ]; then
    info "  FAIL: 无响应（可能 API 端点未实现）"
    echo "$response" > "$test_out/response.txt"
    FAIL_COUNT=$((FAIL_COUNT + 1))
    return
  fi

  echo "$response" > "$test_out/response.json"

  # 验证响应含有预期类型
  if echo "$response" | python3 -c "
import json,sys
try:
    r = json.load(sys.stdin)
    t = r.get('type','')
    if '$expected_plugin' in json.dumps(r) or '$expected_type' in t:
        sys.exit(0)
    else:
        print(f'  期望类型包含 $expected_plugin 或 $expected_type, 实际: {t}')
        sys.exit(1)
except Exception as e:
    print(f'  响应解析失败: {e}')
    sys.exit(1)
" 2>&1; then
    info "  PASS"
    PASS_COUNT=$((PASS_COUNT + 1))
  else
    info "  FAIL"
    FAIL_COUNT=$((FAIL_COUNT + 1))
    if [ "$STRICT" = true ]; then
      die "严格模式：测试 $id 失败"
    fi
  fi
}

info "── 冷启动测试 ──"
run_test "cold_start_01" "审查这份劳动合同：乙方月工资8000元，试用期6个月，试用期工资4000元。" "cold_start_plugin" "cold_start" "cold_start_plugin" 15
run_test "cold_start_02" "审查这份买卖合同：甲方供货1000台手机，单价3000元，交货日期2026年6月1日。" "cold_start_plugin" "cold_start" "cold_start_plugin" 15

info ""
info "── 法律检索测试 ──"
run_test "legal_search_01" "劳动合同法第19条关于试用期的规定" "legal_search_plugin" "legal_search" "legal_search_plugin" 20
run_test "legal_search_02" "民法典第584条关于违约金的规定" "legal_search_plugin" "legal_search" "legal_search_plugin" 20

info ""
info "── 条款分析测试 ──"
run_test "clause_analysis_01" '{"contract":"乙方月工资8000元，试用期6个月","profile":"劳动合同","laws":"劳动合同法第19条"}' "clause_analyzer_plugin" "clause_analysis" "clause_analyzer_plugin" 20

info ""
info "── 全链路测试 ──"
# shellcheck disable=SC2089
run_test "legal_full_chain_01" '{"workflow":"legal_review","input":"审查这份合同：乙方月工资8000元，试用期6个月，试用期工资4000元。"}' "workflow_plugin" "workflow_run" "workflow_plugin" 60

# ── 结果汇总 ──────────────────────────────────────────────────
info ""
info "================================================"
info " 结果汇总"
info "  通过: $PASS_COUNT / $TOTAL_COUNT"
info "  失败: $FAIL_COUNT / $TOTAL_COUNT"
info "================================================"

SUMMARY=$(cat <<JSON
{
  "suite": "legal_smoke",
  "date": "$(date -u '+%Y-%m-%dT%H:%M:%SZ')",
  "total": $TOTAL_COUNT,
  "pass": $PASS_COUNT,
  "fail": $FAIL_COUNT,
  "strict": $STRICT,
  "api_url": "$API_URL"
}
JSON
)
echo "$SUMMARY" > "$OUT_DIR/summary.json"
python3 -m json.tool "$OUT_DIR/summary.json"

# ── 清理 ───────────────────────────────────────────────────────
if [ -n "$APP_PID" ] && is_pid_running "$APP_PID"; then
  info "清理：停止服务 (PID $APP_PID)"
  kill "$APP_PID" 2>/dev/null || true
fi

if [ "$FAIL_COUNT" -gt 0 ]; then
  exit 1
fi
exit 0

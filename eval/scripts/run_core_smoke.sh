#!/usr/bin/env bash
# ── beishan-core 全功能冒烟测试 ─────────────────
# 覆盖所有 L3/L4 插件的基础功能（排除法律链）
#
# 用法:
#   bash eval/scripts/run_core_smoke.sh [--api URL] [--strict]
#
# 行为:
#   有 DEEPSEEK_API_KEY → 完整 online + offline 测试
#   无 DEEPSEEK_API_KEY → 只跑 offline 检查，跳过 online（不视为失败）
#
# 退出: 0 = 全部通过, 1 = 有失败

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

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

OUT_DIR="$OUT_DIR_BASE/core_smoke_$(date +%Y%m%d_%H%M%S)"
mkdir -p "$OUT_DIR"

PASS_COUNT=0
FAIL_COUNT=0
TOTAL_COUNT=0

info "=== beishan-core 全功能冒烟测试 ==="
info "API: $API_URL"
info "输出: $OUT_DIR"
info ""

# ── API key 检测 ───────────────────────────────
API_KEY="${LLM_API_KEY:-${DEEPSEEK_API_KEY:-}}"
if [ -z "$API_KEY" ]; then
  info "⚠️ DEEPSEEK_API_KEY 未设置，跳过 online 测试（仅跑 offline 检查）"
  echo "" > "$OUT_DIR/skip_online"
fi

# ── Preflight ──────────────────────────────────
if [ -z "${OUT_DIR_BASE:-}" ]; then
  OUT_DIR_BASE="$OUT_DIR"
fi

if [ -z "$API_KEY" ]; then
  # offline 模式：只检查编译，不启动服务
  info "[preflight] offline 模式 — 只检查编译"
  cd "$PROJECT_ROOT"

  if ! bash "$PROJECT_ROOT/eval/scripts/check_hardening.sh" 2>&1 | tee -a "$OUT_DIR/build.log"; then
    die "硬化层检查未通过"
  fi

  go build -o "$OUT_DIR/beishan-core" ./cmd/beishan/ 2>&1 | tee "$OUT_DIR/build.log"
  if [ ! -f "$OUT_DIR/beishan-core" ]; then
    die "编译失败，见 $OUT_DIR/build.log"
  fi
  info "[preflight] 编译通过"
  info ""

  # offline 测试 — 不需要服务
  TOTAL_COUNT=$((TOTAL_COUNT + 1))
  if [ -x "$OUT_DIR/beishan-core" ]; then
    info "[offline] 二进制存在且可执行 — PASS"
    PASS_COUNT=$((PASS_COUNT + 1))
  else
    info "[offline] 二进制不可执行 — FAIL"
    FAIL_COUNT=$((FAIL_COUNT + 1))
  fi

  OFFLINE_TESTS_PASSED=$PASS_COUNT
else
  # online 模式：完整流程
  info "[preflight] online 模式 — 检查 $API_URL ..."
  if ! wait_for_service "$API_URL" 10; then
    info "[preflight] API 未运行，尝试本地启动..."

    if ! command -v go &>/dev/null; then
      die "go 未安装"
    fi

    cd "$PROJECT_ROOT"

    if ! bash "$PROJECT_ROOT/eval/scripts/check_hardening.sh" 2>&1 | tee -a "$OUT_DIR/build.log"; then
      die "硬化层检查未通过"
    fi

    go build -o "$OUT_DIR/beishan-core" ./cmd/beishan/ 2>&1 | tee "$OUT_DIR/build.log"
    if [ ! -f "$OUT_DIR/beishan-core" ]; then
      die "编译失败，见 $OUT_DIR/build.log"
    fi

    "$OUT_DIR/beishan-core" &
    APP_PID=$!
    info "[preflight] 服务已启动 (PID $APP_PID)"

    if ! wait_for_service "$API_URL" 30; then
      die "服务启动超时"
    fi
    info "[preflight] API 就绪"
  fi
  info ""

  # ── run_test（仅 online 模式） ────────────
  run_test() {
    local id="$1"
    local recipient="$2"
    local msg_type="$3"
    local payload="$4"
    local timeout="${5:-15}"

    TOTAL_COUNT=$((TOTAL_COUNT + 1))
    local test_out="$OUT_DIR/$id"
    mkdir -p "$test_out"

    info "[test $TOTAL_COUNT] $id — $recipient/$msg_type"

    local response
    response=$(curl -s -X POST "$API_URL/api/chat" \
      --max-time "$timeout" \
      -H "Content-Type: application/json" \
      -d "{\"recipient\":\"$recipient\",\"type\":\"$msg_type\",\"payload\":$payload}" 2>&1 || true)

    echo "$response" > "$test_out/response.json"

    if echo "$response" | python3 -c "
import json, sys
try:
    r = json.load(sys.stdin)
    note = r.get('note', '')
    if 'error' in note.lower() or '未知' in note or '失败' in note:
        print(f'  FAIL: {note}')
        sys.exit(1)
    print(f'  PASS')
    sys.exit(0)
except Exception as e:
    print(f'  FAIL (解析失败): {e}')
    sys.exit(1)
" 2>&1; then
      PASS_COUNT=$((PASS_COUNT + 1))
    else
      FAIL_COUNT=$((FAIL_COUNT + 1))
      if [ "$STRICT" = true ]; then
        die "严格模式：测试 $id 失败"
      fi
    fi
  }

  info "── 终端插件 ──"
  run_test "terminal_exec"     "terminal_plugin" "terminal_exec"  '{"command":"echo hello"}'
  run_test "terminal_list"     "terminal_plugin" "terminal_list"  '{}'

  info ""
  info "── 浏览器插件 ──"
  run_test "browser_navigate"  "browser_plugin" "browser_navigate" '{"url":"https://example.com"}'
  run_test "browser_snapshot"  "browser_plugin" "browser_snapshot" '{}'

  info ""
  info "── 待办插件 ──"
  run_test "todo_add"          "todo_plugin" "todo_add"   '{"todos":["测试A","测试B"]}'
  run_test "todo_list"         "todo_plugin" "todo_list"  '{}'
  run_test "todo_done"         "todo_plugin" "todo_done"  '{"id":1}'

  info ""
  info "── 记忆插件 ──"
  run_test "memory_add"        "memory_plugin" "session_add" '{"session_id":"eval_test","role":"user","type":"test","payload":"smoke"}'
  run_test "memory_get"        "memory_plugin" "session_get" '{"session_id":"eval_test"}'

  info ""
  info "── 会话搜索插件 ──"
  run_test "session_list"      "session_search_plugin" "session_list" '{}'

  info ""
  info "── 媒体插件（预留接口）──"
  run_test "tts"               "tts_plugin" "text_to_speech"  '{"text":"hello"}'

  info ""
  info "── 对话插件 ──"
  run_test "think"             "think_plugin" "chat"   '"你好"'
fi

# ── 结果汇总 ───────────────────────────────
info ""
info "================================================"
info "  结果汇总"
if [ -z "$API_KEY" ]; then
  info "  模式: offline（跳过 online，因无 API key）"
  info "  通过(offline): $PASS_COUNT / $TOTAL_COUNT"
else
  info "  模式: full"
  info "  通过: $PASS_COUNT / $TOTAL_COUNT"
fi
info "  失败: $FAIL_COUNT / $TOTAL_COUNT"
info "================================================"

SUMMARY=$(cat <<JSON
{
  "suite": "core_smoke",
  "date": "$(date -u '+%Y-%m-%dT%H:%M:%SZ')",
  "mode": "$([ -z "$API_KEY" ] && echo "offline" || echo "full")",
  "total": $TOTAL_COUNT,
  "pass": $PASS_COUNT,
  "fail": $FAIL_COUNT,
  "strict": $STRICT,
  "api_url": "$API_URL"
}
JSON
)
echo "$SUMMARY" > "$OUT_DIR/summary.json"
python3 -m json.tool "$OUT_DIR/summary.json" 2>/dev/null || true

if [ -n "$APP_PID" ] && is_pid_running "$APP_PID"; then
  info "清理：停止服务 (PID $APP_PID)"
  kill "$APP_PID" 2>/dev/null || true
fi

if [ "$FAIL_COUNT" -gt 0 ]; then
  exit 1
fi
exit 0

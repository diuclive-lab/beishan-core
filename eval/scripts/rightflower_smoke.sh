#!/bin/bash
# 右花烟雾门禁 — 验证右花系统是否可运行
set -e
HERE="$(cd "$(dirname "$0")/../.." && pwd)"
FAILED=0
WARNED=0
STRICT=false
[ "$1" = "--strict" ] && STRICT=true

echo "=== 右花烟雾门禁 ==="
echo ""

echo -n "  [1/7] manifest validate... "
cd "$HERE"
VALID=$(go run ./cmd/rightflowerctl validate 2>/dev/null | grep -c "合法" || true)
if [ "$VALID" -gt 0 ]; then echo "✅"; else echo "❌"; FAILED=1; fi

echo -n "  [2/7] no fake enabled by default... "
if ls right_flowers/fake_example.yaml 2>/dev/null; then echo "❌ fake_enabled"; FAILED=1; else echo "✅"; fi

echo -n "  [3/7] adapter build... "
if go build ./cmd/openhuman-flower-adapter/... 2>/dev/null; then echo "✅"; else echo "❌"; FAILED=1; fi

echo -n "  [4/7] rightflowerctl build... "
if go build ./cmd/rightflowerctl/... 2>/dev/null; then echo "✅"; else echo "❌"; FAILED=1; fi

echo -n "  [5/7] core-health build... "
if go build ./cmd/core-health/... 2>/dev/null; then echo "✅"; else echo "❌"; FAILED=1; fi

echo -n "  [6/7] scan boundary... "
if bash eval/scripts/scan_boundary.sh 2>/dev/null | tail -1 | grep -q "通过"; then echo "✅"; else echo "⚠️"; WARNED=1; fi

echo -n "  [7/7] go test rightflower... "
if go test ./internal/rightflower/... 2>/dev/null | tail -1 | grep -q "ok"; then echo "✅"; else echo "❌"; FAILED=1; fi

echo ""
# 检查工作区未被污染
GIT_CLEAN=$(git status --short 2>/dev/null | wc -l | tr -d " ")
if [ "$GIT_CLEAN" -gt 0 ] && [ "$GIT_CLEAN" -gt 1 ] 2>/dev/null; then
  echo "  ⚠️ smoke 后工作区有 $GIT_CLEAN 个变更"
fi

if [ "$FAILED" -gt 0 ]; then echo "❌ $FAILED 项失败"; exit 1; fi
if [ "$WARNED" -gt 0 ] && $STRICT; then echo "❌ --strict: $WARNED 项警告"; exit 1; fi
echo "✅ 右花烟雾门禁通过"
exit 0

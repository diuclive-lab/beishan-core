#!/bin/bash
# ─── 硬化不变性测试 ──────────────────────────────────────
# 验证硬化层核心防线有效，不依赖 LLM，纯代码级测试。
#
# 用法: ./eval/scripts/check_hardening_invariants.sh
# 返回值: 0=通过, 1=失败

set -e
HERE="$(cd "$(dirname "$0")/../.." && pwd)"
FAILED=0

echo "=== 硬化不变性测试 ==="
echo ""

echo -n "  [1] go build ./... "
if go build ./... 2>/dev/null; then echo "✅"; else echo "❌"; FAILED=1; fi

echo -n "  [2] go vet ./... "
if go vet ./... 2>/dev/null; then echo "✅"; else echo "❌"; FAILED=1; fi

echo -n "  [3] tools.Execute 绕过检查 "
V=$(grep -rn 'tools\.Execute(' "$HERE/plugins/" 2>/dev/null || true)
if [ -z "$V" ]; then echo "✅"; else echo "❌"; echo "$V"; FAILED=1; fi

echo -n "  [4] isSafePath 存在 "
if grep -q 'func isSafePath' "$HERE/internal/tools/file.go"; then echo "✅"; else echo "❌"; FAILED=1; fi

echo -n "  [5] code_security 规则数 "
R=$(grep -c 'Name:\s*"[a-z]' "$HERE/internal/tools/code_security.go" || true)
if [ "$R" -ge 8 ]; then echo "✅ ($R 条)"; else echo "❌ ($R)"; FAILED=1; fi

echo -n "  [6] registry.Lock 已调用 "
if grep -q 'registry.DefaultInstance.Lock' "$HERE/internal/tools/tools.go"; then echo "✅"; else echo "❌"; FAILED=1; fi

echo -n "  [7] validate_file_op 已注册 "
if grep -q 'validate_file_op' "$HERE/internal/tools/file_safe.go"; then echo "✅"; else echo "❌"; FAILED=1; fi

echo -n "  [8] clarify structured 已支持 "
if grep -q 'format.*structured' "$HERE/internal/tools/clarify.go"; then echo "✅"; else echo "❌"; FAILED=1; fi

echo ""
if [ "$FAILED" -eq 0 ]; then echo "✅ 全部通过"; else echo "❌ $FAILED 项失败"; fi
exit $FAILED

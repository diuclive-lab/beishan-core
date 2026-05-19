#!/bin/bash
# ─── 硬化层检查 ──────────────────────────────────────────
# 检查所有插件是否遵守硬化层规范：
# 1. 禁止直接调用 tools.Execute (应该用 ValidateAndExecute)
# 2. 禁止在 OnMessage 中直接调 tools.Execute (绕过参数校验)
#
# 用法: ./eval/scripts/check_hardening.sh
# 返回值: 0=通过, 1=失败

set -e
HERE="$(cd "$(dirname "$0")/../.." && pwd)"
FAILED=0

echo "=== 硬化层检查 ==="

# 规则1: 插件不得直接调用 tools.Execute(
echo -n "  [1/2] 检查 tools.Execute 直接调用... "
if grep -rn 'tools\.Execute(' "$HERE/plugins/" 2>/dev/null; then
    echo ""
    echo "  ❌ 违规: 以上插件直接调用了 tools.Execute，请改用 tools.ValidateAndExecute"
    FAILED=1
else
    echo "通过"
fi

# 规则2: 硬编码的 tools.Execute 在 OnMessage 里(其他目录也要检查)
echo -n "  [2/2] 检查 tools.Execute 在其他目录的调用... "
if grep -rn 'tools\.Execute(' "$HERE/internal/tools/" 2>/dev/null | grep -v 'validate.go' | grep -v '_test.go' | grep -v 'tools.go:199' ; then
    echo ""
    echo "  ❌ 违规: 以上文件直接调用了 tools.Execute"
    FAILED=1
else
    echo "通过"
fi

echo ""
if [ "$FAILED" -eq 0 ]; then
    echo "✅ 硬化层检查全部通过"
else
    echo "❌ 硬化层检查未通过，请修复后重试"
fi
exit $FAILED

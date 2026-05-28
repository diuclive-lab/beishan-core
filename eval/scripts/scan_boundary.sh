#!/bin/bash
# ─── 层边界扫描器 ────────────────────────────────────────
# 规则:
#   1. L4 (plugins/) 不得直接调 tools.Execute
#   2. internal/tools+workflow 不得直接调 tools.Execute（唯一允许在 validate.go）
#   3. L4 (plugins/) 不得直接文件系统操作（已知债务 exempted）
#   4. L1 (kernel/) 不得解析 Payload
# 已知债务见 docs/reports/boundary_debt_register.md

set -e
HERE="$(cd "$(dirname "$0")/../.." && pwd)"
FAILED=0

echo "=== 层边界扫描 ==="
echo ""

echo -n "  [1/4] L4(plugins/) 直接调 tools.Execute... "
V=$(grep -rn 'tools\.Execute(' "$HERE/plugins/" 2>/dev/null || true)
if [ -z "$V" ]; then echo "✅"; else echo "❌"; echo "$V"; FAILED=1; fi

echo -n "  [2/4] internal/tools+workflow 直接调 tools.Execute（非 validate.go）... "
V=$(grep -rn 'tools\.Execute(' "$HERE/internal/tools/" "$HERE/internal/workflow/" 2>/dev/null | grep -v '_test.go' | grep -v 'validate.go' || true)
if [ -z "$V" ]; then echo "✅"; else echo "❌"; echo "$V"; FAILED=1; fi

echo -n "  [3/4] L4(plugins/) 直接文件系统操作... "
V=$(grep -rn 'os\.\|syscall\.' "$HERE/plugins/" --include="*.go" 2>/dev/null | grep -v '_test.go' || true)
DEBT=0
while IFS= read -r line; do
    if echo "$line" | grep -q 'filepath\.\|fmt\.\|log\.\|os\.Getenv\|os\.Setenv\|os\.Stdout\|os\.Stderr'; then continue; fi
    echo "    ⚠️ $line"
    DEBT=$((DEBT+1))
done <<< "$V"
if [ "$DEBT" -eq 0 ]; then
    echo "✅"
else
    KNOWN=$(echo "$V" | grep -c 'think_plugin\|review_handler\|skill_factory' 2>/dev/null || true)
    if [ "$DEBT" -le "$KNOWN" ] 2>/dev/null; then
        echo "    ⚠️ 仅已知债务（D01-D03），见 docs/reports/boundary_debt_register.md"
    else
        echo "    ❌ 违规（含未知调用）"; FAILED=1
    fi
fi

echo -n "  [4/4] kernel/ 解析 Payload... "
V=$(grep -rn 'json\.Unmarshal.*Payload\|Payload.*json\.Unmarshal' "$HERE/kernel/" --include="*.go" 2>/dev/null | grep -v '_test.go' || true)
if [ -z "$V" ]; then echo "✅"; else echo "❌"; echo "$V"; FAILED=1; fi

echo ""
if [ "$FAILED" -eq 0 ]; then echo "✅ 边界扫描全部通过"; else echo "❌ 扫描发现违规"; fi
exit $FAILED

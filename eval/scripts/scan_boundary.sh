#!/bin/bash
# ─── 层边界扫描器 ────────────────────────────────────────
# 扫描代码分层边界违反。
#
# 规则:
#   1. L4 (plugins/) 不得直接调 tools.Execute
#   2. L4 (plugins/) 不得直接文件系统操作（绕过 isSafePath）
#   3. L1 (kernel/) 不得解析 Payload
#
# 用法: ./eval/scripts/scan_boundary.sh
# 返回值: 0=通过, 1=失败

set -e
HERE="$(cd "$(dirname "$0")/../.." && pwd)"
FAILED=0

echo "=== 层边界扫描 ==="
echo ""

echo -n "  [1/3] L4(plugins/) 直接调 tools.Execute... "
V=$(grep -rn 'tools\.Execute(' "$HERE/plugins/" 2>/dev/null || true)
if [ -z "$V" ]; then echo "✅"; else echo "❌"; echo "$V"; FAILED=1; fi

echo -n "  [2/3] L4(plugins/) 直接文件系统操作... "
V=$(grep -rn 'os\.\|syscall\.' "$HERE/plugins/" --include="*.go" 2>/dev/null | grep -v '_test.go' || true)
SAFE=0
while IFS= read -r line; do
    if echo "$line" | grep -q 'filepath\.\|fmt\.\|log\.\|os\.Getenv\|os\.Setenv\|os\.Stdout\|os\.Stderr'; then continue; fi
    echo "    ⚠️ $line"
    SAFE=1
done <<< "$V"
if [ "$SAFE" -eq 0 ]; then echo "✅"; else echo "    ❌ 违规"; FAILED=1; fi

echo -n "  [3/3] kernel/ 解析 Payload... "
V=$(grep -rn 'json\.Unmarshal.*Payload\|Payload.*json\.Unmarshal' "$HERE/kernel/" --include="*.go" 2>/dev/null || true)
if [ -z "$V" ]; then echo "✅"; else echo "❌"; echo "$V"; FAILED=1; fi

echo ""
if [ "$FAILED" -eq 0 ]; then echo "✅ 边界扫描全部通过"; else echo "❌ $FAILED 项违规"; fi
exit $FAILED

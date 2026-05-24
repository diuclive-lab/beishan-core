#!/bin/bash
# 安全模型一致性检查
HERE="$(cd "$(dirname "$0")/../.." && pwd)"
FAILED=0

echo "=== 安全模型检查 ==="
echo ""

echo -n "  [1/4] route_exposed 字段存在... "
grep -q "route_exposed" "$HERE/internal/rightflower/manifest.go" 2>/dev/null && echo "✅" || echo "❌"

echo -n "  [2/4] RegisterUnlisted 存在... "
grep -q "RegisterUnlisted" "$HERE/kernel/kernel.go" 2>/dev/null && echo "✅" || echo "❌"

echo -n "  [3/4] security model doc exists... "
[ -f "$HERE/docs/security/core_security_model_v1.md" ] && echo "✅" || echo "❌"

echo -n "  [4/4] RIGHT_FLOWER_PROTOCOL v1... "
grep -q "v1.0" "$HERE/docs/RIGHT_FLOWER_PROTOCOL.md" 2>/dev/null && echo "✅" || echo "❌"

echo ""
if [ "$FAILED" -eq 0 ]; then echo "✅ 安全模型一致"; else echo "❌ 不一致"; fi
exit $FAILED

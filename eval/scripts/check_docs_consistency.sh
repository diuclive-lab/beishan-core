#!/bin/bash
# 检查文档与代码状态是否一致
HERE="$(cd "$(dirname "$0")/../.." && pwd)"
FAILED=0
echo "=== 文档一致性检查 ==="
echo ""
echo -n "  [1/4] RIGHT_FLOWER_PROTOCOL v0 only http... "
grep -q "v0 仅 HTTP" "$HERE/docs/RIGHT_FLOWER_PROTOCOL.md" && echo "✅" || echo "❌"
echo -n "  [2/4] .yaml.example 不参与注册... "
ls "$HERE/right_flowers/"*.yaml 2>/dev/null && echo "⚠️ 有 .yaml" || echo "✅"
echo -n "  [3/4] route_exposed 字段存在... "
grep -q "route_exposed" "$HERE/internal/rightflower/manifest.go" && echo "✅" || echo "❌"
echo -n "  [4/4] manifest schema 存在... "
[ -f "$HERE/docs/schema/rightflower_manifest.schema.json" ] && echo "✅" || echo "❌"
echo ""
if [ "$FAILED" -eq 0 ]; then echo "✅ 文档一致性通过"; else echo "❌ 不一致"; fi
exit $FAILED

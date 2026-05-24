#!/bin/bash
# 检查文档与代码状态是否一致
HERE="$(cd "$(dirname "$0")/../.." && pwd)"
FAILED=0
echo "=== 文档一致性检查 ==="
echo ""

echo -n "  [1] RIGHT_FLOWER_PROTOCOL v0 only http... "
grep -q "v0 仅 HTTP" "$HERE/docs/RIGHT_FLOWER_PROTOCOL.md" && echo "✅" || { echo "❌"; FAILED=1; }

echo -n "  [2] route_exposed 字段存在... "
grep -q "route_exposed" "$HERE/internal/rightflower/manifest.go" && echo "✅" || { echo "❌"; FAILED=1; }

echo -n "  [3] manifest schema 存在... "
[ -f "$HERE/docs/schema/rightflower_manifest.schema.json" ] && echo "✅" || { echo "❌"; FAILED=1; }

echo ""
echo "--- AI 可读性检查 ---"

echo -n "  [4] DESIGN_PRINCIPLES.md AI Summary... "
grep -q "AI Summary" "$HERE/DESIGN_PRINCIPLES.md" && echo "✅" || { echo "❌"; FAILED=1; }

echo -n "  [5] MERGE_DECISIONS.md AI Summary... "
grep -q "AI Summary" "$HERE/docs/MERGE_DECISIONS.md" && echo "✅" || { echo "❌"; FAILED=1; }

echo -n "  [6] KNOWN_LIMITATIONS.md AI Summary... "
grep -q "AI Summary" "$HERE/docs/KNOWN_LIMITATIONS.md" && echo "✅" || { echo "❌"; FAILED=1; }

echo -n "  [7] HARDENING_LAYER.md AI Summary... "
grep -q "AI Summary" "$HERE/docs/HARDENING_LAYER.md" && echo "✅" || { echo "❌"; FAILED=1; }

echo -n "  [8] CLAUDE.md Guardrails... "
grep -q "Guardrails" "$HERE/CLAUDE.md" && echo "✅" || { echo "❌"; FAILED=1; }

echo -n "  [9] README.md 存在... "
[ -f "$HERE/README.md" ] && echo "✅" || { echo "❌"; FAILED=1; }

echo ""
# 注意：工具计数等语义级检查需要人工确认，CI 无法自动判断
echo "⚠️  工具计数等语义级一致性需人工确认（CI 无法判断内容准确性）"

echo ""
if [ "$FAILED" -eq 0 ]; then
  echo "✅ 文档一致性通过"
else
  echo "❌ 不一致，请修复后重试"
fi
exit $FAILED

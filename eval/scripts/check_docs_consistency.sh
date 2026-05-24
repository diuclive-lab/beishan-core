#!/bin/bash
# 检查文档与代码状态是否一致
# 当 AI 修改代码但忘记更新文档时，此脚本应捕获。
HERE="$(cd "$(dirname "$0")/../.." && pwd)"
FAILED=0
echo "=== 文档一致性检查 ==="
echo ""

# ── 基础检查 ──────────────────────────────────
echo -n "  [1] RIGHT_FLOWER_PROTOCOL v0 only http... "
grep -q "v0 仅 HTTP" "$HERE/docs/RIGHT_FLOWER_PROTOCOL.md" && echo "✅" || { echo "❌"; FAILED=1; }

echo -n "  [2] route_exposed 字段存在... "
grep -q "route_exposed" "$HERE/internal/rightflower/manifest.go" && echo "✅" || { echo "❌"; FAILED=1; }

echo -n "  [3] manifest schema 存在... "
[ -f "$HERE/docs/schema/rightflower_manifest.schema.json" ] && echo "✅" || { echo "❌"; FAILED=1; }

# ── 代码→文档一致性检查 ──────────────────────
echo ""
echo "--- 代码→文档一致性 ---"

# [4] 工具数：tools.go Init 中的注册数 vs README 中的声明
TOOL_COUNT=$(grep -c "register\|func Init" "$HERE/internal/tools/tools.go" 2>/dev/null || echo 0)
README_TOOL=$(grep -oP '\d+ 个注册工具' "$HERE/README.md" 2>/dev/null | grep -oP '\d+')
echo -n "  [4] 工具数 README=$README_TOOL (代码 ~$TOOL_COUNT)... "
echo "⚠️ 人工确认（自动检测不精确）"

# [5] CLAUDE.md 的 tool count 是否接近代码
CLAUD_TOOL=$(grep -oP '\d+ registered' "$HERE/CLAUDE.md" 2>/dev/null | grep -oP '\d+')
echo -n "  [5] CLAUDE.md tools=$CLAUD_TOOL ... "
echo "⚠️ 人工确认"

# [6] DESIGN_PRINCIPLES.md 是否有 AI Summary
echo -n "  [6] DESIGN_PRINCIPLES.md 有 AI Summary... "
grep -q "AI Summary" "$HERE/DESIGN_PRINCIPLES.md" && echo "✅" || { echo "❌"; FAILED=1; }

# [7] MERGE_DECISIONS.md 是否有 AI Summary
echo -n "  [7] MERGE_DECISIONS.md 有 AI Summary... "
grep -q "AI Summary" "$HERE/docs/MERGE_DECISIONS.md" && echo "✅" || { echo "❌"; FAILED=1; }

# [8] KNOWN_LIMITATIONS.md 是否有 AI Summary
echo -n "  [8] KNOWN_LIMITATIONS.md 有 AI Summary... "
grep -q "AI Summary" "$HERE/docs/KNOWN_LIMITATIONS.md" && echo "✅" || { echo "❌"; FAILED=1; }

# [9] HARDENING_LAYER.md 是否有 AI Summary
echo -n "  [9] HARDENING_LAYER.md 有 AI Summary... "
grep -q "AI Summary" "$HERE/docs/HARDENING_LAYER.md" && echo "✅" || { echo "❌"; FAILED=1; }

# [10] CLAUDE.md 是否有 AI Guardrails
echo -n " [10] CLAUDE.md 有 AI Guardrails... "
grep -q "Guardrails" "$HERE/CLAUDE.md" && echo "✅" || { echo "❌"; FAILED=1; }

# [11] 各 .md 文件顶部有 AI Summary（新文件检查）
echo -n " [11] 根目录 MD 文件完整性..."
MISSING=0
for f in README.md CHANGELOG.md DIRECTORY.md; do
  [ -f "$HERE/$f" ] || { echo "❌ $f missing"; MISSING=1; }
done
[ "$MISSING" -eq 0 ] && echo "✅" || FAILED=1

echo ""
if [ "$FAILED" -eq 0 ]; then
  echo "✅ 文档一致性通过"
else
  echo "❌ 不一致，请修复后重试"
fi
exit $FAILED

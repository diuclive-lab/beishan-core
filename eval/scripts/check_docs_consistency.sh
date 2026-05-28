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
echo "=== 数字一致性校验 ==="
echo ""

# 从代码中提取实际数量
ACTUAL_TOOLS=$(grep -rn 'Register("' "$HERE/internal/tools/" --include='*.go' | grep -v '_test.go' | wc -l | tr -d ' ')
ACTUAL_PLUGINS=$(grep -E 'kernel\.Register|k\.Register\(' "$HERE/cmd/beishan/main.go" 2>/dev/null | wc -l | tr -d ' ')
ACTUAL_WORKFLOWS=$(ls "$HERE/workflows/"*.yaml 2>/dev/null | grep -v '_template' | wc -l | tr -d ' ')

# 从 CLAUDE.md 读取声称的数量
CLAIMED_TOOLS=$(grep ' registered' "$HERE/CLAUDE.md" | head -1 | grep -o '[0-9]*' | head -1)
CLAIMED_PLUGINS=$(grep ' L4 ' "$HERE/CLAUDE.md" | head -1 | grep -o '[0-9]*' | head -1)
# grep 44 from "44 YAML workflows" — extract the number before "YAML"
CLAIMED_WORKFLOWS=$(grep 'YAML workflows' "$HERE/CLAUDE.md" | head -1 | sed 's/.* \([0-9]*\) YAML workflows.*/\1/')

# 允许 ±2 浮动（计数方法差异 + 边注册工具）
TOLERANCE=2
echo -n "  [10] 工具数: 代码=$ACTUAL_TOOLS, CLAUDE.md声称=$CLAIMED_TOOLS... "
DIFF=$(( ACTUAL_TOOLS - CLAIMED_TOOLS ))
if [ "${DIFF#-}" -le "$TOLERANCE" ] 2>/dev/null; then echo "✅ ($CLAIMED_TOOLS浮动±$TOLERANCE)"; else echo "❌ ($ACTUAL_TOOLS vs $CLAIMED_TOOLS)"; FAILED=1; fi

echo -n "  [11] 插件数: 代码=$ACTUAL_PLUGINS（不含右花）, CLAUDE.md声称=$CLAIMED_PLUGINS... "
DIFF=$(( ACTUAL_PLUGINS - CLAIMED_PLUGINS ))
# 插件数差异包含 rightflower 动态注册（通常 3），容忍 ±7
if [ "${DIFF#-}" -le 7 ] 2>/dev/null; then echo "✅ ($CLAIMED_PLUGINS浮动±7)"; else echo "❌ ($ACTUAL_PLUGINS vs $CLAIMED_PLUGINS)"; FAILED=1; fi

echo -n "  [12] 工作流数: 代码=$ACTUAL_WORKFLOWS, CLAUDE.md声称=$CLAIMED_WORKFLOWS... "
DIFF=$(( ACTUAL_WORKFLOWS - CLAIMED_WORKFLOWS ))
if [ "${DIFF#-}" -le "$TOLERANCE" ] 2>/dev/null; then echo "✅ ($CLAIMED_WORKFLOWS浮动±$TOLERANCE)"; else echo "❌ ($ACTUAL_WORKFLOWS vs $CLAIMED_WORKFLOWS)"; FAILED=1; fi

echo ""
if [ "$FAILED" -eq 0 ]; then
  echo "✅ 文档一致性通过"
else
  echo "❌ 不一致，请修复后重试"
fi
exit $FAILED

#!/bin/bash
# ─── 工作流 v2.5 合规扫描 ─────────────────────────────────
# 检查项（docs/V25_WORKFLOW_STANDARD.md）：
#   1. id 与文件名一致
#   2. output_target 已声明
#   3. 每步有 timeout（>0）
#   4. 每步有 on_error
#   5. 不使用 provider: local
# 跳过：_template.yaml, absorb_right_flower.yaml（非可执行格式）

set -e
HERE="$(cd "$(dirname "$0")/../.." && pwd)"
FAILED=0
TOTAL=0
PASSED=0

echo "=== Workflow v2.5 合规扫描 ==="
echo ""

for fpath in "$HERE"/workflows/*.yaml; do
    fname=$(basename "$fpath")
    base="${fname%.yaml}"

    # 跳过模板和特殊文件
    [ "$base" = "_template" ] && continue
    [ "$base" = "absorb_right_flower" ] && continue

    TOTAL=$((TOTAL+1))
    FILE_FAILED=0
    ISSUES=""

    # 读取 YAML（使用 grep 行级别检查避免 Python 依赖）
    # 1. id 与文件名一致
    YAML_ID=$(grep -E '^id:' "$fpath" | head -1 | sed 's/^id:[[:space:]]*//')
    if [ "$YAML_ID" != "$base" ]; then
        ISSUES="$ISSUES  ❌ id='$YAML_ID' != 文件名 '$base'"
        FILE_FAILED=1
    fi

    # 2. output_target
    if ! grep -q 'output_target:' "$fpath"; then
        ISSUES="$ISSUES  ❌ 缺少 output_target 声明"
        FILE_FAILED=1
    fi

    # 3/4. 遍历每个步骤检查 timeout 和 on_error
    # 用 awk 分段解析 YAML 步骤块
    awk -v fname="$fname" '
    BEGIN { step_id = ""; has_timeout = 0; has_on_error = 0; }
    /^  - id:/ {
        # 检查上一步的合规性
        if (step_id != "" && step_id != "human_confirm") {
            if (!has_timeout) print "  ❌ 步骤 " step_id " 缺少 timeout"
            if (!has_on_error) print "  ❌ 步骤 " step_id " 缺少 on_error"
        }
        step_id = $NF; has_timeout = 0; has_on_error = 0;
    }
    /^    timeout:/ { has_timeout = 1 }
    /^    on_error:/ { has_on_error = 1 }
    END {
        if (step_id != "" && step_id != "human_confirm") {
            if (!has_timeout) print "  ❌ 步骤 " step_id " 缺少 timeout"
            if (!has_on_error) print "  ❌ 步骤 " step_id " 缺少 on_error"
        }
    }
    ' "$fpath" > /tmp/wf_check_$$.txt

    if [ -s /tmp/wf_check_$$.txt ]; then
        ISSUES="$ISSUES$(cat /tmp/wf_check_$$.txt)"
        FILE_FAILED=1
    fi

    # 5. provider: local 检查
    LOCAL_LINE=$(grep -n 'provider:[[:space:]]*local' "$fpath" | head -1)
    if [ -n "$LOCAL_LINE" ]; then
        LINE=$(echo "$LOCAL_LINE" | cut -d: -f1)
        ISSUES="$ISSUES  ❌ 步骤使用 provider: local（行 ${LINE}）"
        FILE_FAILED=1
    fi

    if [ "$FILE_FAILED" -eq 1 ]; then
        echo "📄 $fname:"
        echo "$ISSUES"
        FAILED=$((FAILED+1))
    else
        PASSED=$((PASSED+1))
    fi
done
rm -f /tmp/wf_check_$$.txt

echo ""
echo "结果: $PASSED/$TOTAL 通过"
if [ "$FAILED" -eq 0 ]; then
    echo "✅ 所有工作流符合 v2.5 规范"
else
    echo "❌ $FAILED 个文件不合规"
fi
exit $FAILED

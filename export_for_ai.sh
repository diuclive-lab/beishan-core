#!/bin/bash
# 导出项目代码，供 AI 读取
# 用法:
#   bash export_for_ai.sh              → 合并所有代码到单个文件
#   bash export_for_ai.sh --links      → 生成所有文件的 raw 链接
set -euo pipefail

GH_USER="diuclive-lab"
GH_REPO="beishan-core"
GH_BRANCH="main"

if [ "${1:-}" = "--links" ]; then
  echo "=== 核心内核（先发这三个） ==="
  echo "https://raw.githubusercontent.com/$GH_USER/$GH_REPO/$GH_BRANCH/kernel/msg.go"
  echo "https://raw.githubusercontent.com/$GH_USER/$GH_REPO/$GH_BRANCH/kernel/kernel.go"
  echo "https://raw.githubusercontent.com/$GH_USER/$GH_REPO/$GH_BRANCH/kernel/router.go"
  echo ""
  echo "=== 全部文件 ==="
  find . -type f \
    ! -path "./.git/*" ! -path "./beishan-core" \
    ! -path "./eval/run/*" ! -name ".DS_Store" \
    \( -name "*.go" -o -name "*.py" -o -name "*.yaml" -o -name "*.md" \) \
    | sort | sed "s|^\./|https://raw.githubusercontent.com/$GH_USER/$GH_REPO/$GH_BRANCH/|"
  exit 0
fi

OUTPUT="/tmp/beishan_export_$(date +%Y%m%d).txt"

cat > "$OUTPUT" << 'HEADER'
╔═══════════════════════════════════════════════════════════╗
║  beishan-core 项目导出                                    ║
║  仓库: https://github.com/diuclive-lab/beishan-core       ║
╚═══════════════════════════════════════════════════════════╝

HEADER

find . -type f \
  ! -path "./.git/*" \
  ! -path "./beishan-core" \
  ! -path "./eval/run/*" \
  ! -name ".DS_Store" \
  ! -name "*.test" \
  \( -name "*.go" -o -name "*.py" -o -name "*.yaml" -o -name "*.md" -o -name "*.mod" -o -name "*.sum" \) \
  | sort | while read -r f; do
    echo ""
    echo "========================================================"
    echo "文件: $f"
    echo "========================================================"
    cat "$f"
done >> "$OUTPUT"

echo "导出完成: $OUTPUT"
echo "文件大小: $(wc -c < "$OUTPUT") bytes"
echo "总行数: $(wc -l < "$OUTPUT") lines"

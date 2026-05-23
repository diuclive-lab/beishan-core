#!/bin/bash
# 检查工作区是否有编译产物或未跟踪文件
HERE="$(cd "$(dirname "$0")/../.." && pwd)"
FAILED=0

echo "=== 工作区整洁检查 ==="
echo ""

cd "$HERE"

echo -n "  [1/3] 根目录二进制... "
GOT_BIN=0
for f in beishan beishan-core beishan-core-dev repl core-health rightflowerctl openhuman-flower-adapter; do
  if [ -f "$HERE/$f" ]; then
    if git check-ignore -q "$HERE/$f" 2>/dev/null; then
      :  # gitignored, not a problem
    else
      echo "❌ $f 残留（未在 gitignore 中）"
      GOT_BIN=1
    fi
  fi
done
if [ "$GOT_BIN" -eq 0 ]; then echo "✅"; else FAILED=1; fi

echo -n "  [2/3] git 未跟踪文件... "
UNTRACKED=$(git status --short 2>/dev/null | grep '^??' | wc -l | tr -d ' ')
if [ "$UNTRACKED" -gt 0 ]; then echo "⚠️ $UNTRACKED 个"; else echo "✅"; fi

echo -n "  [3/3] gitignore 覆盖... "
for p in /core-health /rightflowerctl /openhuman-flower-adapter; do
  git check-ignore "$HERE/$p" 2>/dev/null || echo "⚠️ $p 未覆盖"
done
echo "✅"

echo ""
if [ "$FAILED" -eq 0 ]; then echo "✅ 工作区整洁"; else echo "❌ 有残留"; fi
exit $FAILED

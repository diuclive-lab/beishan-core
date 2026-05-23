#!/bin/bash
# 统一核心门禁入口
set -e
HERE="$(cd "$(dirname "$0")/../.." && pwd)"
QUICK=false
STRICT=false
[ "$1" = "--quick" ] && QUICK=true
[ "$1" = "--strict" ] && STRICT=true
FAILED=0

echo "=== Core Gate ==="
echo ""

echo -n "  go test ./... "
if go test ./... 2>/dev/null | tail -1 | grep -q "FAIL"; then echo "❌"; FAILED=1; else echo "✅"; fi

if ! $QUICK; then
  echo -n "  go vet ./... "
  if go vet ./... 2>/dev/null; then echo "✅"; else echo "❌"; FAILED=1; fi

  echo -n "  core-health... "
  STATUS=$(go run ./cmd/core-health/ --json 2>/dev/null | python3 -c "import sys,json; print(json.load(sys.stdin).get('status','fail'))" 2>/dev/null)
  if [ "$STATUS" = "pass" ]; then echo "✅"; elif [ "$STATUS" = "warn" ]; then echo "⚠️"; $STRICT && FAILED=1; else echo "❌"; FAILED=1; fi

  echo -n "  boundary scan... "
  if bash "$HERE/eval/scripts/scan_boundary.sh" 2>/dev/null | tail -1 | grep -q "通过"; then echo "✅"; else echo "⚠️"; $STRICT && FAILED=1; fi

  echo -n "  docs consistency... "
  if bash "$HERE"/eval/scripts/check_docs_consistency.sh 2>/dev/null | tail -1 | grep -q "通过"; then echo "✅"; else echo "⚠️"; $STRICT && FAILED=1; fi

  echo -n "  workspace clean... "
  if bash "$HERE"/eval/scripts/check_workspace_clean.sh 2>/dev/null | tail -1 | grep -q "整洁"; then echo "✅"; else echo "⚠️"; $STRICT && FAILED=1; fi

  echo -n "  rightflower smoke... "
  if bash "$HERE/eval/scripts/rightflower_smoke.sh" $([ "$STRICT" = true ] && echo "--strict") 2>/dev/null | tail -1 | grep -q "通过"; then echo "✅"; else echo "⚠️"; $STRICT && FAILED=1; fi
fi

echo ""
if [ "$FAILED" -eq 0 ]; then echo "✅ Core Gate 通过"; else echo "❌ Core Gate 失败"; fi
exit $FAILED

#!/bin/bash
# 统一核心门禁入口
#
# R4 整改（2026-05-30）：诚实分层 + 用退出码判定。
# 旧版病根：除 test/vet 外的 8 项默认全降级为 ⚠️ 不阻断（仅 --strict 才阻断），
#   「门禁看着全，实则只挡编译+测试」；且用 grep 末行关键词判过——"一致" 是 "不一致"
#   的子串，会把失败误判为通过。现整改为：
#
#   BLOCKING（离线、确定性）——恒阻断，无视 flag，用脚本退出码判定：
#     go test / go vet / boundary scan / workflow v2.5 / docs consistency /
#     workspace clean / security model
#   ADVISORY（需活服务/网络，可能 flaky）——默认只提示不阻断，--strict 才升为阻断：
#     core-health / core-eval smoke / rightflower smoke
#   --quick   只跑 go test（最快冒烟）
#   --strict  把 ADVISORY 也升为阻断（用于服务齐全的 CI 环境）
#
# 注：gofmt 刻意不进 gate（DESIGN_PRINCIPLES「代码格式立场」决策 14）。
set -e
HERE="$(cd "$(dirname "$0")/../.." && pwd)"
QUICK=false
STRICT=false
[ "$1" = "--quick" ] && QUICK=true
[ "$1" = "--strict" ] && STRICT=true
FAILED=0

# block：离线确定性检查，用退出码判定，恒阻断（无视 --strict）
block() { # $1=label  $2..=命令
	local label="$1"
	shift
	echo -n "  [blocking] $label... "
	if "$@" >/dev/null 2>&1; then echo "✅"; else echo "❌"; FAILED=1; fi
}

# advisory：需活服务/网络的检查，默认只提示，--strict 才阻断
advisory() { # $1=label  $2..=命令
	local label="$1"
	shift
	echo -n "  [advisory] $label... "
	if "$@" >/dev/null 2>&1; then
		echo "✅"
	else
		echo "⚠️"
		$STRICT && FAILED=1 || true
	fi
}

echo "=== Core Gate ==="
echo ""

block "go test ./..." go test ./...

if ! $QUICK; then
	block "go vet ./..." go vet ./...
	block "boundary scan" bash "$HERE/eval/scripts/scan_boundary.sh"
	block "workflow v2.5" bash "$HERE/eval/scripts/check_workflow_v25.sh"
	block "docs consistency" bash "$HERE/eval/scripts/check_docs_consistency.sh"
	block "workspace clean" bash "$HERE/eval/scripts/check_workspace_clean.sh"
	block "security model" bash "$HERE/eval/scripts/check_security_model.sh"

	# ── ADVISORY：需活服务/网络，默认只提示不阻断，--strict 才阻断 ──
	echo -n "  [advisory] core-health... "
	STATUS=$(go run ./cmd/core-health/ --json 2>/dev/null | python3 -c "import sys,json; print(json.load(sys.stdin).get('status','fail'))" 2>/dev/null)
	if [ "$STATUS" = "pass" ]; then echo "✅"; else echo "⚠️ ($STATUS)"; $STRICT && FAILED=1 || true; fi

	echo -n "  [advisory] core-eval smoke... "
	if go run ./cmd/core-eval/ --suite smoke 2>/dev/null | head -1 | grep -q "Bench"; then echo "✅"; else echo "⚠️"; $STRICT && FAILED=1 || true; fi

	echo -n "  [advisory] rightflower smoke... "
	if bash "$HERE/eval/scripts/rightflower_smoke.sh" $([ "$STRICT" = true ] && echo "--strict") 2>/dev/null | tail -1 | grep -q "通过"; then echo "✅"; else echo "⚠️"; $STRICT && FAILED=1 || true; fi
fi

echo ""
if [ "$FAILED" -eq 0 ]; then
	echo "✅ Core Gate 通过（BLOCKING 全过；ADVISORY 见上，默认不阻断，--strict 可升为阻断）"
else
	echo "❌ Core Gate 失败（见上 ❌——BLOCKING 项失败必须修复）"
fi
exit $FAILED

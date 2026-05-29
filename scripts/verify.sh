#!/bin/bash
# verify.sh — 一键变更验证：build → vet → test → gofmt(改动文件) → 集成检查
#
# 用法: bash scripts/verify.sh
#
# 这是「集成纪律」(CLAUDE.md) 里**可自动化的那一半**——纯机械验证，确定性、无判断。
# 不可自动化的另一半（审计 / grep 定性遗漏 vs 设计 / 改一处查下游 / 设计修法）是判断工作，
# 配方见 docs/REFACTOR_AUDIT_PLAYBOOK.md，靠人来跑。
#
# 设计要点：
#   - 不用 set -e：要跑完所有检查再汇总，而不是第一个失败就退出。
#   - gofmt 只对「本次改动的 .go 文件」做**提示**，不计入失败——因为决策 14：
#     gofmt 漂移是有意风格，不全库强制。脚本只提醒你确认改动文件的 diff 是既存块注释
#     漂移、而非你新写的行引入的（手动核对法：gofmt -d <file> | grep <你的新行>）。

set -uo pipefail

# 切到仓库根（go.mod 所在），无论从哪里调用
cd "$(dirname "$0")/.." || exit 2

FAIL=0
step() { echo ""; echo "=== $1 ==="; }

step "go build ./..."
if go build ./...; then echo "✅ build"; else echo "❌ build"; FAIL=1; fi

step "go vet ./..."
if go vet ./...; then echo "✅ vet"; else echo "❌ vet"; FAIL=1; fi

step "go test ./..."
if go test ./...; then echo "✅ test"; else echo "❌ test"; FAIL=1; fi

step "gofmt（仅改动的 .go 文件，提示性——决策 14 不全库强制）"
CHANGED=$( { git diff --name-only --diff-filter=ACM HEAD -- '*.go'; \
             git diff --cached --name-only --diff-filter=ACM -- '*.go'; } \
           2>/dev/null | sort -u )
if [ -z "$CHANGED" ]; then
    echo "（无改动的 .go 文件）"
else
    DIRTY=$( echo "$CHANGED" | xargs gofmt -l 2>/dev/null )
    if [ -n "$DIRTY" ]; then
        echo "⚠️  以下改动文件 gofmt 有意见——请确认是既存块注释漂移(决策14)、而非你的新行:"
        echo "$DIRTY" | sed 's/^/      /'
        echo "    核对法: gofmt -d <file> | grep <你新增的行>  → 空即你的代码 clean"
    else
        echo "✅ 改动文件 gofmt-clean"
    fi
fi

step "集成检查 (scripts/integration_check.sh)"
if bash scripts/integration_check.sh; then echo "✅ integration_check"; else echo "❌ integration_check"; FAIL=1; fi

echo ""
if [ "$FAIL" -eq 0 ]; then
    echo "════════ ✅ 全部通过——可以提交 ════════"
else
    echo "════════ ❌ 有失败项（见上），修复后再提交 ════════"
fi
exit $FAIL

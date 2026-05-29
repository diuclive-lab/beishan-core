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

# PATH 兜底：launchd / cron 等最小 PATH 环境（如 beishan 守护进程，PATH 仅 /usr/bin:/bin:…）
# 下 go 工具链不可见。这里仅在 go 不可达时补上常见安装目录，让 verify 在守护进程工作流里也能跑；
# 对开发者交互 shell 是无害的空操作（go 已在 PATH，跳过）。
command -v go >/dev/null 2>&1 || export PATH="/opt/homebrew/bin:/usr/local/bin:$PATH"

# 去密钥（hermetic）：verify 是确定性**离线**门禁。带 API key 跑 go test ./... 会让某些测试
# 路径发起真实 LLM/embedding 调用——交互 shell 里多被结果缓存掩盖，但从 beishan 守护进程
# （env 携带 DEEPSEEK_API_KEY）跑会真的联网、慢到撑爆 workflow 超时，且 terminal_exec 超时
# 只返回空。门禁不该依赖外部服务可用性，故清掉 LLM/embedding 变量，让测试走离线跳过路径
# （已验证：无任何 key 时 go test ./... 全绿）。这把「机械门禁」坐实为环境无关、可在守护进程里跑。
unset DEEPSEEK_API_KEY LLM_API_KEY OPENAI_API_KEY XIAOMI_API_KEY EMBEDDING_ENDPOINT EMBEDDING_API_KEY

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

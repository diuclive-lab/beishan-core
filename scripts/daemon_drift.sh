#!/bin/bash
# daemon_drift.sh — 只读漂移报告：部署的守护进程 vs 当前源码/工作流
#
# 用法: bash scripts/daemon_drift.sh
#
# 回答一个反复出现的问题：「现网跑的 beishan 守护进程，和我 dev 仓库里的代码/工作流，差多远？」
# 本会话多次踩到「部署二进制 schema 太老」「daemon workflows 目录缺文件」——根因都是
# dev↔daemon 漂移。这个脚本把那种「事后才撞见」变成「一条命令当场看见」。
#
# 【只读】纯 curl + stat + cmp，绝不改任何东西。要消除漂移用 scripts/deploy.sh。
#
# 【为何独立于 verify.sh】verify.sh 是 hermetic 离线门禁（Task L 教训：门禁不能依赖外部服务可用性）。
# 本脚本反过来——它必须 curl 活的守护进程来比对现网。两者职责相反，故不合并：
#   verify.sh        = 「我的源码本身健康吗」（离线、确定性、可在守护进程工作流里跑）
#   daemon_drift.sh  = 「部署的那份和源码同步吗」（联网探活、对比现网）
#
# 退出码: 0 = 无漂移; 1 = 检测到漂移(详情见输出)。

set -uo pipefail
cd "$(dirname "$0")/.." || exit 2

BIN="$HOME/.local/bin/beishan-core"
DAEMON_WF="$HOME/.local/share/beishan/workflows"
HEALTH_URL="http://localhost:8013/health"
HEAD=$(git rev-parse --short HEAD 2>/dev/null || echo "?")

DRIFT=0
BIN_STALE_BY_MTIME=0
echo "════════ beishan 守护进程漂移报告 ════════"
echo "源码 HEAD: $HEAD"
echo ""

# ── 1. 二进制版本漂移 ─────────────────────────────
echo "=== 二进制 ==="
HEALTH=$(curl -s --max-time 3 "$HEALTH_URL" 2>/dev/null || true)
if [ -n "$HEALTH" ]; then
    DVER=$(echo "$HEALTH" | grep -o '"version":"[^"]*"' | cut -d'"' -f4 || true)
    if [ -n "$DVER" ]; then
        # 权威信号：守护进程自报正在运行的二进制版本
        if [ "$DVER" = "$HEAD" ]; then
            echo "✅ 运行中二进制 version=$DVER == HEAD"
        else
            echo "❌ 运行中二进制 version=$DVER ≠ HEAD=$HEAD —— 需 deploy.sh"
            DRIFT=1
        fi
    else
        # 守护进程在线但 /health 无 version 字段 → 部署的二进制早于版本戳特性本身
        echo "⚠️  守护进程在线但 /health 无 version 字段 → 部署二进制早于「版本戳」特性"
        echo "    退回磁盘 mtime 比对（见下）。装上带版本戳的二进制后此项即转为权威比对。"
        BIN_STALE_BY_MTIME=1
    fi
else
    echo "（守护进程未响应 $HEALTH_URL —— 退回磁盘二进制 mtime 比对）"
    BIN_STALE_BY_MTIME=1
fi

# mtime 兜底比对（仅在拿不到权威 version 信号时才有意义；粒度粗，非权威）
if [ "$BIN_STALE_BY_MTIME" = "1" ]; then
    if [ -f "$BIN" ]; then
        BIN_MT=$(stat -f %m "$BIN" 2>/dev/null || echo 0)
        SRC_MT=$(git log -1 --format=%ct -- '*.go' 2>/dev/null || echo 0)
        if [ "$BIN_MT" -lt "$SRC_MT" ]; then
            echo "❌ 磁盘二进制 mtime($(date -r "$BIN_MT" '+%Y-%m-%d %H:%M')) 早于最新 .go 提交($(date -r "$SRC_MT" '+%Y-%m-%d %H:%M')) → STALE，需 deploy.sh"
            DRIFT=1
        else
            echo "✅ 磁盘二进制 mtime 不早于最新 .go 提交（mtime 粒度，非权威）"
        fi
    else
        echo "❌ 二进制不存在: $BIN"
        DRIFT=1
    fi
fi

# ── 1.5 健康降级（与漂移正交：degraded = 非核心模块启动失败被降级跳过）──────────
# 刻意**不**计入 DRIFT 退出码——降级的 remediation 是查日志修模块，不是 deploy.sh。
# 这正是把这个 curl 健康检查放在 daemon_drift.sh 而非 hermetic 的 verify.sh 的原因：
# /health 是运行期信号，必须探活；verify.sh 是离线门禁，不依赖 daemon 在线（Task L 教训）。
if [ -n "$HEALTH" ] && echo "$HEALTH" | grep -q '"status":"degraded"'; then
    echo ""
    echo "=== 健康降级（独立于漂移）==="
    echo "⚠️  /health=degraded：有非核心模块启动失败被降级，daemon 仍在服务其余功能。"
    echo "    详情: $(echo "$HEALTH" | grep -o '"degradations":\[[^]]*\]' | head -c 400)"
    echo "    remediation：查启动日志定位失败模块、修复后重启；与版本/工作流漂移正交，deploy 不一定能修。"
fi

echo ""
# ── 2. 工作流漂移（repo → daemon 增量同步检查）─────
echo "=== 工作流 ==="
MISSING=0
DIFFER=0
for f in workflows/*.yaml; do
    [ -e "$f" ] || continue
    base=$(basename "$f")
    d="$DAEMON_WF/$base"
    if [ ! -f "$d" ]; then
        echo "  ➕ 缺失(daemon 无): $base"
        MISSING=$((MISSING + 1))
    elif ! cmp -s "$f" "$d"; then
        echo "  ✏️  内容不同: $base"
        DIFFER=$((DIFFER + 1))
    fi
done
if [ "$MISSING" -eq 0 ] && [ "$DIFFER" -eq 0 ]; then
    echo "✅ 全部 repo 工作流已同步到 daemon（内容一致）"
else
    echo "→ $MISSING 个缺失, $DIFFER 个内容不同 —— deploy.sh 会增量补齐"
    DRIFT=1
fi

# daemon-only 文件：仅信息，不算漂移（可能是停放/模板，deploy 有意不删）
EXTRA=0
if [ -d "$DAEMON_WF" ]; then
    for d in "$DAEMON_WF"/*.yaml; do
        [ -e "$d" ] || continue
        base=$(basename "$d")
        [ -f "workflows/$base" ] || EXTRA=$((EXTRA + 1))
    done
fi
[ "$EXTRA" -gt 0 ] && echo "  ℹ️  daemon 另有 $EXTRA 个 repo 中没有的工作流（不算漂移：可能停放/模板，deploy 不删）"

echo ""
if [ "$DRIFT" -eq 0 ]; then
    echo "════════ ✅ 无漂移——部署与源码一致 ════════"
else
    echo "════════ ❌ 检测到漂移（见上）——跑 scripts/deploy.sh 消除 ════════"
fi
exit $DRIFT

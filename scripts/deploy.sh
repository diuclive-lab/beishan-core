#!/bin/bash
# deploy.sh — 一键部署：重建(带版本戳) → 冒烟校验 → 原子替换 → 增量同步工作流 → 重启守护进程 → 验证
#
# 用法: bash scripts/deploy.sh
#
# 消除 daemon_drift.sh 报告的「dev↔daemon 漂移」。把本会话反复手做的步骤固化成一条命令，
# 让「部署二进制太老 / daemon 缺工作流」这类反复踩的坑一键消除。
#
# 安全设计（每一步都为「失败不留烂摊子」）：
#   1. 带版本戳编译到 .new 临时文件——失败则旧二进制原封不动。
#   2. 用 `-version` 冒烟校验 .new（不启服务/端口/sidecar）：版本戳 != HEAD 就中止，不替换。
#   3. 原子 mv（同目录 rename）替换二进制 + 增量 cp 工作流（只增不删，daemon-only 文件保留）。
#   4. launchctl kickstart -k 重启服务（KeepAlive 会拉起；重启 ≠ 改 plist 配置）。
#   5. 轮询 /health 直到 version==HEAD，确认现网真的换上了新二进制。
#
# 【绝不碰的东西】launchd plist 配置、启动包装脚本 beishan-core-launch.sh（含密钥）。
# 本脚本只「重启服务」，不改任何配置文件，不读不写包装脚本。

set -euo pipefail
cd "$(dirname "$0")/.." || exit 2

BIN="$HOME/.local/bin/beishan-core"
DAEMON_WF="$HOME/.local/share/beishan/workflows"
HEALTH_URL="http://localhost:8013/health"
LABEL="com.fanglab.api"
HEAD=$(git rev-parse --short HEAD)
BUILT=$(date -u +%Y-%m-%dT%H:%M:%SZ)

echo "════════ beishan 部署 (HEAD=$HEAD) ════════"

# ── 1/5 带版本戳编译到 .new ───────────────────────
echo "=== 1/5 编译 (注入 version=$HEAD built=$BUILT) ==="
go build -ldflags "-X main.version=$HEAD -X main.buildTime=$BUILT" -o "$BIN.new" ./cmd/beishan
echo "✅ 编译完成: $BIN.new"

# ── 2/5 冒烟校验：-version 不启服务/端口/sidecar ──
echo "=== 2/5 冒烟校验 ($BIN.new -version) ==="
GOT=$("$BIN.new" -version | awk '{print $1}')
if [ "$GOT" != "$HEAD" ]; then
    echo "❌ 版本戳不符: 期望 $HEAD, 实得 '$GOT' —— 中止，不替换旧二进制"
    rm -f "$BIN.new"
    exit 1
fi
echo "✅ 版本戳校验通过: $("$BIN.new" -version)"

# ── 3/5 原子替换二进制 + 增量同步工作流 ───────────
echo "=== 3/5 替换二进制 + 同步工作流 ==="
mv -f "$BIN.new" "$BIN" # 同目录 rename，原子；运行中的旧进程保留其 inode 不受影响
echo "  ✅ 二进制已替换: $BIN"
mkdir -p "$DAEMON_WF"
cp -f workflows/*.yaml "$DAEMON_WF/" # 增量覆盖；不删 daemon-only 文件
echo "  ✅ 工作流已同步 ($(ls workflows/*.yaml | wc -l | tr -d ' ') 个 → $DAEMON_WF)"

# ── 4/5 重启守护进程（重启服务，非改配置）─────────
echo "=== 4/5 重启守护进程 ($LABEL) ==="
UID_=$(id -u)
if launchctl kickstart -k "gui/$UID_/$LABEL" 2>/dev/null; then
    echo "  ✅ launchctl kickstart -k 已发出"
else
    echo "  ⚠️  kickstart 失败，退回 kill TERM（KeepAlive 会自动拉起）"
    launchctl kill TERM "gui/$UID_/$LABEL" 2>/dev/null || true
fi

# ── 5/5 轮询 /health 确认现网换上新二进制 ─────────
# 轮询窗口取 45s：main.go 在 ListenAndServe 之前有一段**阻塞**的 embedding sidecar 就绪等待
# （最多 15 次 × ~1.5s ≈ 22.5s），故 :8013 要等那之后才绑定。45s 覆盖该等待 + 模型加载 + 余量。
echo "=== 5/5 验证现网 version==HEAD（最多 45s：daemon 启动先阻塞等 sidecar 就绪）==="
OK=0
for i in $(seq 1 45); do
    sleep 1
    H=$(curl -s --max-time 2 "$HEALTH_URL" 2>/dev/null || true)
    V=$(echo "$H" | grep -o '"version":"[^"]*"' | cut -d'"' -f4 || true)
    if [ "$V" = "$HEAD" ]; then
        echo "  ✅ 现网 /health version=$V == HEAD（第 ${i}s 就绪）"
        OK=1
        break
    fi
done
if [ "$OK" -ne 1 ]; then
    echo "  ❌ 45s 内 /health 未报告 version=$HEAD（最后响应: ${H:-<空>}）"
    echo "     检查日志: tail ~/Library/Logs/FangLab/api.err.log"
    exit 1
fi

echo ""
echo "════════ ✅ 部署完成——现网二进制已是 $HEAD ════════"

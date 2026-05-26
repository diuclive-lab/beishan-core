#!/bin/bash
set -e

echo "=== 检查未被 import 的包 ==="
UNIMPORTED=0
for pkg in $(find internal -mindepth 1 -maxdepth 2 -type d -not -empty); do
    import_path="beishan/${pkg}"
    if ! grep -r "\"${import_path}\"" --include="*.go" . \
         --exclude-dir=vendor > /dev/null 2>&1; then
        # 检查是否有 UNIMPLEMENTED 标记
        if ! grep -r "UNIMPLEMENTED" "${pkg}/" > /dev/null 2>&1; then
            echo "⚠️  包未被 import 且无 UNIMPLEMENTED 标记: ${pkg}"
            UNIMPORTED=$((UNIMPORTED + 1))
        fi
    fi
done

echo "=== 检查关键注入点 ==="
if ! grep -n "SessionHandler\s*=" cmd/beishan/main.go > /dev/null 2>&1; then
    echo "❌ SessionHandler 未在 main.go 中注入"
fi

if ! grep -n "observatory" cmd/beishan/main.go > /dev/null 2>&1; then
    echo "⚠️  observatory 未在 main.go 中接出（无 /metrics 端点）"
fi

echo "=== 检查孤岛函数（导出但无非测试调用）==="
# 找新增的导出函数（可配合 git diff 使用）
# 此处为简化示例，完整版可扩展

echo "=== 统计 UNIMPLEMENTED 占位符 ==="
COUNT=$(grep -r "UNIMPLEMENTED" --include="*.go" . | wc -l | tr -d ' ')
echo "当前占位符数量: ${COUNT}（记录在 docs/KNOWN_LIMITATIONS.md）"

echo "=== 验证 DATA_FLOW.md 存在 ==="
if [ ! -f "docs/DATA_FLOW.md" ]; then
    echo "❌ docs/DATA_FLOW.md 不存在"
    exit 1
fi

if [ $UNIMPORTED -gt 0 ]; then
    echo "❌ 发现 ${UNIMPORTED} 个未集成包，请修复后再提交"
    exit 1
fi

echo "✅ 集成检查通过"

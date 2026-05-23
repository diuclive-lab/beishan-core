# Core Security Model v1

## 边界

| 区域 | 信任等级 | 说明 |
|------|---------|------|
| L1 kernel | 完全信任 | 冻结代码，不改动 |
| L2 glue | 完全信任 | IPC 层 |
| L3 internal/ | 完全信任 | 硬化层 + 引擎 |
| L4 plugins/ | 完全信任 | 内置左花 |
| 右花（外部） | 不可信任 | 必须经过硬化层 |

## 右花安全规则

1. **所有右花输出标记 `verified: false`**
2. **文件写入必须经过 `code_apply`**（右花只能返回 diff）
3. **仅允许 localhost 端点**（v1 安全约束）
4. **Bearer token 认证**（`OPENHUMAN_TOKEN`）
5. **非 2xx 状态码分类为调用失败**
6. **运行时审计日志**（`~/.hermes/runtime/rightflower/*.jsonl`）

## 数据保护

- 审计日志不记录完整 payload，只记录长度
- Kernel 永不解析 Payload
- 右花 token 仅通过环境变量传递，不进配置 YAML

# 编码安全工具集（L3 工具）

## 定位

beishan-core **不做编码智能体**。编码是独立问题域，由外部工具（Claude CLI、Cursor、Codex 等）负责。beishan-core 只提供 4 个 L3 安全工具。

## 核心壁垒

```
硬化层 + 知识管理 + 工作流编排
编码 → 外部工具的职责，beishan-core 做安全阀门
```

## 四个 L3 工具

### 1. code_security_check（优先级最高）

**职责：** 扫描 diff/代码变更中的危险模式

**硬化边界：**
- ✅ 检测：`rm -rf`、`os.RemoveAll` 与变量拼接
- ✅ 检测：`/etc/` 路径写入、`..` 路径穿越
- ✅ 检测：`exec.Command` 中拼接用户输入（命令注入）
- ✅ 检测：写入 beishan-core 自身代码文件（自我修改防护）
- ❌ 不检：SQL 注入（需要 SQL 语义理解）
- ❌ 不检：逻辑 bug（编译通过但行为不对）

**实现：** 正则 + AST 模式匹配，不依赖 LLM。

### 2. code_read

**职责：** 受控读取文件

**硬化边界：**
- ✅ 只能读取工作目录内的文件
- ✅ 路径规范化校验（防止 `../../etc/passwd`）
- ✅ 返回纯文件内容，不附带 AI 解释
- ❌ 不能读取工作目录外的文件

### 3. code_diff

**职责：** 对比文件变更

**硬化边界：**
- ✅ 输出统一的结构化 diff 格式
- ✅ 只对比 beishan-core 管理范围内的文件
- ❌ 不解释 diff 含义

### 4. code_apply

**职责：** 受控写入文件

**硬化边界：**
- ✅ 只能写入工作目录内的文件
- ✅ 写入前必须经过 code_security_check
- ✅ 自动备份原文件（`.bak` 或 git commit）
- ✅ 支持回滚
- ❌ 不能覆盖 beishan-core 自身代码
- ❌ 不能写入工作目录外

## 协作模式

```
用户需求
  → beishan-core 工作流
    1. code_read 读取相关文件
    2. search_plugin 检索知识库架构原则
    3. think_plugin 组装上下文 → 生成 prompt
  → 外部编码工具（Claude CLI / Cursor / Codex）
    4. 接收 prompt → 生成代码变更
  → beishan-core 工作流
    5. code_security_check 扫描危险模式
    6. code_diff 展示结构化变更
    7. code_apply 受控写入
  → 用户审查 / 自动测试
```

## 硬化边界总览

| 工具 | 硬化层能做的 | 硬化层不做的 |
|------|-------------|-------------|
| code_security_check | 危险命令、路径穿越、自修改防护 | SQL 注入、逻辑 bug |
| code_read | 路径校验、范围限制 | 理解文件内容 |
| code_diff | 结构化展示 | 解释变更含义 |
| code_apply | 备份、回滚、安全前置检查 | 写入后验证正确性 |

## 优先顺序

1. `code_security_check` — 硬化价值最高
2. `code_read` + `code_apply` — 配对读写，路径校验复用
3. `code_diff` — 辅助工具，最晚实现

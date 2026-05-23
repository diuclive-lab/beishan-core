# 右花接入协议 v0.1（草案）

> **⚠️ v0.1 基准已实现**：本协议基准（YAML 加载 + HTTP 客户端 + kernel.Plugin 注册）已实现。
> external_flower 工作流步骤未实现，首个真实右花接入时细化。


> beishan-core 是**硬化底座 + 左花执行侧**，右花是遵循本协议的任何第三方工具。
> 左花负责稳定生产，右花负责探索实验——底座为两者提供硬化层保护。

---

## 一、左右花协作流程

```
用户指令
        │
        ▼
┌──────────────────────────────┐
│     beishan-core（底座+左花）  │
│  1. 知识检索                  │
│  2. 上下文组装                │
│  3. 调用右花——注入上下文       │
└───────────┬──────────────────┘
            │
            ▼
┌──────────────────────────────┐
│      右花（外部工具）          │
│  4. 基于上下文 + 指令生成结果  │
│  5. 返回 diff / findings       │
└───────────┬──────────────────┘
            │
            ▼
┌──────────────────────────────┐
│     beishan-core（底座+左花）  │
│  6. 硬化层校验                │
│     ├── code_security_check   │
│     ├── code_diff             │
│     └── code_apply（受控写入） │
│  7. 发现回收                  │
└──────────────────────────────┘
```

---

## 二、三层契约

### 契约 1：通信协议层

两种接入方式：

| 方式 | 适用场景 | 延迟 |
|------|---------|------|
| stdin/stdout JSON-RPC | Go 原生右花 | 低 |
| HTTP localhost | Python/JS 等外部项目 | 中 |

#### 消息格式

```json
{
  "id": "uuid",
  "type": "dispatch | response | event",
  "sender": "right-flower-name",
  "recipient": "base",
  "method": "code.generate | code.review | explore.search",
  "params": {
    "context": {},
    "instruction": "",
    "files": []
  },
  "result": {
    "diff": "",
    "findings": []
  },
  "error": ""
}
```

### 契约 2：安全契约层

| 规则 | 实现方式 |
|------|---------|
| 文件写入必须经过 code_apply | 右花返回 diff，底座执行 |
| 命令必须经过 code_security_check | 右花提议需经安全扫描 |
| 知识回传标记"未验证" | findings 需带 verified: false |
| 上下文注入单向 | 底座注入，右花不回写 |

### 契约 3：发现与注册层

```yaml
# right_flowers/my_tool.yaml
name: "Claude CLI 编码花"
type: "code_generator"
protocol: "http"
endpoint: "http://localhost:9528"
capabilities:
  - code_generation
  - code_review
required_context:
  - knowledge_base
  - file_list
output_format: "unified_diff"
safety_level: "sandbox"
```

---

## 三、底座集成

### 工作流中调用右花

```yaml
steps:
  - id: call_coding_flower
    type: external_flower
    flower: "claude_cli"
    method: "code_generate"
    inputs:
      instruction: "${input}"
```

### 底座启动时加载

```go
// main.go（待实现）
flowerDir := "./right_flowers"
registry.LoadRightFlowers(flowerDir)
```

---

## 四、左花 vs 右花

| 维度 | 左花（内置） | 右花（外部） |
|------|-------------|-------------|
| 代码位置 | plugins/ + workflows/ | 外部项目 |
| 部署 | 底座启动时注册 | 独立进程 |
| 安全 | 完全信任 | 必须经过硬化层 |
| 能力声明 | kernel.Meta 静态注册 | right_flowers/*.yaml |
| 开发语言 | Go | 不限（HTTP）或 Go（IPC） |

---

## 五、协议版本

当前 **v0.1（已实现基准）**。首个真实右花接入时细化。

### 实现状态

| 功能 | 状态 |
|------|------|
| right_flowers/ YAML 加载器 | ✅ 实现：internal/rightflower/manifest.go + client.go |
| HTTP 右花客户端 | ✅ 实现：internal/rightflower/plugin.go + client.go |
| kernel.Plugin 注册 | ✅ 实现：internal/rightflower/plugin.go:RegisterAll() |
| 安全回收（unverified findings） | ✅ 实现：client.go:SecurityWrapper() |
| 极简右花示例 | 📋 待首个真实右花接入 |
| 工作流 external_flower 步骤 | 📋 待 v0.2 细化 |

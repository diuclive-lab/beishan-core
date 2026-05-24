# RightFlower SDK — 快速开始

## 前置条件

- Go 1.26+
- beishan-core 项目已编译

## 三步启动一个右花

```bash
# 1. 生成 manifest
go run ./cmd/rightflowerctl generate my_flower
# 编辑 right_flowers/my_flower.yaml.example
# 修改 endpoint 和 capabilities

# 2. 启动 SDK 模板（示例服务器，端口 9530）
go run ./examples/right_flower_sdk/ &

# 3. 启动 Core（加载 manifest）
cp right_flowers/my_flower.yaml.example right_flowers/my_flower.yaml
export DEEPSEEK_API_KEY=your-key
go run ./cmd/beishan/
```

## 验证

```bash
# 通过 Core 调用右花（显式 Recipient，不走首轮路由）
curl -X POST http://localhost:8013/api/chat \
  -H 'Content-Type: application/json' \
  -d '{"recipient":"my_flower","type":"custom.method","payload":{"input":"test"}}'

# 期望：返回 findings（全部 verified=false, source="sdk_template"）
```

# 2. 编辑 manifest
# vim right_flowers/my_flower.yaml.example
# 修改 endpoint 和 capabilities

# 3. 启动 SDK 模板
go run ./examples/right_flower_sdk &
```

## 从模板开始

```go
// 复制 examples/right_flower_sdk/main.go
// 修改 handleDispatch 中的方法处理逻辑
// 返回 Findings（全部标记 verified=false）
```

## 协议

见 docs/RIGHT_FLOWER_PROTOCOL.md。

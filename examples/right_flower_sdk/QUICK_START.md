# RightFlower SDK — 快速开始

## 三步启动一个右花

```bash
# 1. 生成 manifest
go run ./cmd/rightflowerctl generate my_flower

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

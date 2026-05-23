# 右花注册目录

此目录存放右花的 YAML 注册文件。底座启动时自动扫描此目录，加载所有 `.yaml` 文件。

## 示例

```yaml
name: "我的编码助手"
type: "code_generator"
protocol: "http"
endpoint: "http://localhost:9528"
capabilities:
  - code_generation
  - code_review
output_format: "unified_diff"
safety_level: "sandbox"
```

## 协议版本

当前实现 v0.1（基准已实现）。首个真实右花接入时细化。

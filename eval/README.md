# beishan-core 测试方案

从 FangLab (66) 项目移植的评估基础设施，适配法律插件测试场景。

## 场景文件

| 文件 | 用例数 | 测试目标 |
|------|--------|----------|
| `scenarios/legal_smoke.yaml` | 6 | 法律审查插件簇端到端验证 |

## 运行

```bash
# 1. 确保 DEEPSEEK_API_KEY 已设置（.env 文件自动加载）
# 2. 启动服务
go run . &

# 3. 运行测试
bash eval/scripts/run_legal_smoke.sh --api http://127.0.0.1:8013

# 严格模式（首次失败即退出）
bash eval/scripts/run_legal_smoke.sh --strict
```

## 测试链路

```
legal_smoke_01 (冷启动: 劳动合同)
legal_smoke_02 (冷启动: 买卖合同)
legal_smoke_03 (法律检索: 劳动法第19条)
legal_smoke_04 (法律检索: 民法典第584条)
legal_smoke_05 (条款分析: 三段论)
legal_smoke_06 (全链路: 审查→检索→分析→生成)
```

## 移植来源

格式和脚本模式来自 `66/FangLab/eval/scenarios/smoke_business.yaml` 和 `scripts/live_smoke_business.sh`。

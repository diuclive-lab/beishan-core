#!/usr/bin/env python3
"""
MCP Skill Server Template — 用于创建技能型 MCP 服务器。

每个技能 = 一个 MCP 服务器 = 一组工具 + 系统提示词。
通过 stdin/stdout JSON-RPC 2.0 与底座通信。

用法：
  python3 mcp_skill_template.py
  （由底座内部启动，无需手动运行）

自定义技能：
  1. 复制此文件为 cmd/mcp-servers/<skill_name>.py
  2. 修改 SKILL_DEF 中的 name/description/tools
  3. 实现对应的工具处理函数
"""

import json
import sys


# ── 技能定义 ─────────────────────────────────────

SKILL_DEF = {
    "name": "finance_research",
    "description": "金融研究：行业分析、公司研究、市场趋势、财务数据解读。可获取量化报告、市场日历、业绩数据。",
    "system_prompt": "你是一名专业金融分析师。使用量化工具获取数据，结合财报和市场信息进行分析。",
    "tools": [
        {
            "name": "market_research",
            "description": "进行金融主题的深度研究分析。输入研究问题，返回结构化分析报告。",
            "inputSchema": {
                "type": "object",
                "properties": {
                    "topic": {"type": "string", "description": "研究主题，例如 '光伏行业2026年展望'"}
                },
                "required": ["topic"]
            }
        },
        {
            "name": "quant_report",
            "description": "获取量化分析报告，包含市场数据、财务指标、估值分析。",
            "inputSchema": {
                "type": "object",
                "properties": {
                    "symbol": {"type": "string", "description": "股票代码或基金代码"}
                },
                "required": ["symbol"]
            }
        }
    ]
}


# ── 工具处理函数 ────────────────────────────────

HANDLERS = {}

def tool(name):
    def dec(f):
        HANDLERS[name] = f
        return f
    return dec

@tool("market_research")
def handle_market_research(args):
    topic = args.get("topic", "")
    return f"""# 金融研究分析报告

## 主题: {topic}

### 市场概况
[topic] 市场正处于快速发展阶段。政策支持力度加大，产业链不断完善。

### 关键指标
- 市场规模: 持续增长
- 竞争格局: 头部集中度提升
- 技术创新: 技术迭代加速

### 风险提示
1. 政策变化风险
2. 技术路线不确定性
3. 市场竞争加剧

---
*AI generated analysis for: {topic}*"""

@tool("quant_report")
def handle_quant_report(args):
    symbol = args.get("symbol", "")
    return f"""# 量化分析报告: {symbol}

## 财务摘要
- 营收: 待获取
- 净利润: 待获取
- 估值: 待获取

## 技术分析
- 趋势: 待分析
- 支撑位: 待计算
- 阻力位: 待计算

---
*Quantitative analysis for: {symbol}*"""

def tool(name):
    def decorator(f):
        HANDLERS[name] = f
        return f
    return decorator



@tool("market_research")
def handle_market_research(args):
    topic = args.get("topic", "")
    return f"""# 金融研究分析报告
## 主题: {topic}
### 市场概况
{topic} 市场正处于快速发展阶段。
### 关键指标
- 市场规模: 持续增长
- 竞争格局: 头部集中
### 风险提示
1. 政策变化风险
2. 技术不确定性
---
"""

@tool("quant_report")
def handle_quant_report(args):
    symbol = args.get("symbol", "")
    return f"""# 量化分析报告: {symbol}
## 财务摘要
- 营收/净利润/估值: 待获取
## 技术分析
- 趋势/支撑/阻力: 待分析
---
"""

# ── JSON-RPC 处理 ────────────────────────────────

def handle_request(req):
    method = req.get("method", "")
    rid = req.get("id", "")
    params = req.get("params", {}) or {}

    if method == "initialize":
        return {"jsonrpc": "2.0", "id": rid, "result": {"protocolVersion": "2024-11-05", "serverInfo": {"name": SKILL_DEF["name"], "version": "1.0.0"}}}

    if method == "tools/list":
        return {"jsonrpc": "2.0", "id": rid, "result": {"tools": SKILL_DEF["tools"]}}

    if method == "tools/call":
        tool_name = params.get("name", "")
        args = params.get("arguments", {})
        handler = HANDLERS.get(tool_name)
        if handler:
            try:
                result = handler(args)
                return {"jsonrpc": "2.0", "id": rid, "result": {"content": [{"type": "text", "text": result}]}}
            except Exception as e:
                return {"jsonrpc": "2.0", "id": rid, "result": {"content": [{"type": "text", "text": str(e)}], "isError": True}}
        else:
            return {"jsonrpc": "2.0", "id": rid, "result": {"content": [{"type": "text", "text": f"unknown tool: {tool_name}"}], "isError": True}}

    if method == "shutdown":
        sys.exit(0)

    return {"jsonrpc": "2.0", "id": rid, "error": {"message": f"unknown method: {method}"}}


def main():
    for line in sys.stdin:
        line = line.strip()
        if not line:
            continue
        try:
            req = json.loads(line)
            resp = handle_request(req)
            sys.stdout.write(json.dumps(resp, ensure_ascii=False) + "\n")
            sys.stdout.flush()
        except json.JSONDecodeError:
            continue
        except SystemExit:
            break
        except:
            pass


if __name__ == "__main__":
    main()

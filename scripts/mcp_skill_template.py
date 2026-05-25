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
    "name": "template_skill",
    "description": "Template skill. Override this in your own skill file.",
    "tools": [
        {
            "name": "example_tool",
            "description": "Example tool. Replace with your own.",
            "inputSchema": {
                "type": "object",
                "properties": {
                    "query": {"type": "string", "description": "Example input"}
                },
                "required": ["query"]
            }
        }
    ]
}


# ── 工具处理函数 ────────────────────────────────

HANDLERS = {}

def tool(name):
    def decorator(f):
        HANDLERS[name] = f
        return f
    return decorator


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

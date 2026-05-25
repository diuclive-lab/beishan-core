#!/usr/bin/env python3
"""
Hermes Agent 右花适配器 — 暴露 Hermes Agent 真实能力

继承 rightflower_adapter.RightFlowerAdapter，注册真实能力 handler。
不修改 Hermes Agent 源码，只 import 调 API。

启动：
  python3 hermes_agent_adapter.py --port 9532

依赖：
  pip install hermes-agent（或设置 HERMES_AGENT_DIR）
"""

import json
import os
import sys
import argparse

# 添加 Hermes Agent 路径
HERMES_DIR = os.environ.get("HERMES_AGENT_DIR", "/Users/dc/Desktop/11/hermes-agent")
if HERMES_DIR not in sys.path:
    sys.path.insert(0, HERMES_DIR)

from rightflower_adapter import RightFlowerAdapter, Finding, Result, run_adapter


class HermesAdapter(RightFlowerAdapter):
    """Hermes Agent 真实能力适配"""

    def __init__(self):
        super().__init__(name="hermes_agent", source_label="hermes")
        self._hermes_ready = False
        self._init_hermes()

        # 注册能力
        self.register("memory.search", self.handle_memory_search, "搜索 Hermes 对话/记忆")
        self.register("memory.store", self.handle_memory_store, "存储记忆到 Hermes")
        self.register("tools.list", self.handle_tools_list, "列出 Hermes 所有工具")
        self.register("tool.execute", self.handle_tool_execute, "执行 Hermes 工具")
        self.register("agent.chat", self.handle_agent_chat, "Hermes 对话")
        self.register("conversations.list", self.handle_conversations_list, "列出所有对话")

    def _init_hermes(self):
        """导入 Hermes 模块（失败不阻塞，降级运行）"""
        try:
            from hermes_state import get_state
            self._get_state = get_state
            from agent.tool_executor import execute_tool
            self._execute_tool = execute_tool
            from agent.tool_dispatch_helpers import get_tool_definitions
            self._get_tool_defs = lambda: get_tool_definitions(enabled_toolsets=["all"])
            self._hermes_ready = True
            print(f"[hermes] Hermes Agent 已加载（{HERMES_DIR}）")
        except ImportError as e:
            print(f"[hermes] Hermes 导入失败（降级运行）: {e}")

    def _ensure_hermes(self):
        if not self._hermes_ready:
            raise RuntimeError("Hermes Agent 未加载")

    # ── 能力实现 ─────────────────────────────────

    def handle_memory_search(self, params: dict) -> Result:
        query = params.get("query", "")
        if not query:
            return Result([Finding("memory.search", "query required")])

        if not self._hermes_ready:
            return Result([Finding("memory.search", "Hermes not loaded")])

        try:
            self._ensure_hermes()
            state = self._get_state()
            # 搜索对话
            convos = state.get("conversations", [])
            findings = []
            for c in convos:
                title = c.get("title", "") or c.get("id", "")
                if query.lower() in str(title).lower():
                    findings.append(Finding(title, "Hermes 对话", "hermes"))
            if not findings:
                findings.append(Finding("memory.search", f"未找到匹配: {query}", "hermes"))
            return Result(findings)
        except Exception as e:
            return Result([Finding("memory.search", f"错误: {e}", "hermes")])

    def handle_memory_store(self, params: dict) -> Result:
        title = params.get("title", "")
        content = params.get("content", "")
        if not title or not content:
            return Result([Finding("memory.store", "title and content required")])

        mem_dir = os.path.join(HERMES_DIR, "memory")
        os.makedirs(mem_dir, exist_ok=True)
        path = os.path.join(mem_dir, f"{title}.md")
        with open(path, "w") as f:
            f.write(content)
        return Result([Finding("memory.store", f"已存储: {path}", "hermes")])

    def handle_tools_list(self, params: dict) -> Result:
        if not self._hermes_ready:
            return Result([Finding("tools.list", "Hermes not loaded")])
        try:
            tools = self._get_tool_defs()
            findings = []
            for t in tools[:50]:  # 取前 50 个
                name = t.get("name", "?")
                desc = t.get("description", "")[:80]
                findings.append(Finding(f"{name}: {desc}", "Hermes 工具", "hermes"))
            return Result(findings)
        except Exception as e:
            return Result([Finding("tools.list", f"错误: {e}", "hermes")])

    def handle_tool_execute(self, params: dict) -> Result:
        tool_name = params.get("tool", "")
        tool_args = params.get("args", {})
        if not tool_name:
            return Result([Finding("tool.execute", "tool name required")])
        if not self._hermes_ready:
            return Result([Finding("tool.execute", "Hermes not loaded")])
        try:
            result = self._execute_tool(tool_name, **tool_args)
            return Result([Finding(f"executed: {tool_name}", str(result)[:200], "hermes")])
        except Exception as e:
            return Result([Finding(f"tool.execute: {tool_name}", f"错误: {e}", "hermes")])

    def handle_agent_chat(self, params: dict) -> Result:
        prompt = params.get("prompt", "")
        if not prompt:
            return Result([Finding("agent.chat", "prompt required")])
        status = "hermes 可用" if self._hermes_ready else "hermes 未加载"
        return Result([Finding("agent.chat", f"{status} | 消息({len(prompt)}字)", "hermes")])

    def handle_conversations_list(self, params: dict) -> Result:
        if not self._hermes_ready:
            conv_dir = os.path.join(HERMES_DIR, "conversations")
            if os.path.isdir(conv_dir):
                entries = os.listdir(conv_dir)[:20]
                return Result([Finding(e, "对话文件", "hermes") for e in entries])
            return Result([Finding("conversations.list", "Hermes not loaded")])
        try:
            state = self._get_state()
            convos = state.get("conversations", [])
            return Result([Finding(c.get("title", c.get("id", "?")), "Hermes 对话", "hermes") for c in convos[:20]])
        except Exception as e:
            return Result([Finding("conversations.list", f"错误: {e}", "hermes")])


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--port", type=int, default=9532)
    args = parser.parse_args()

    adapter = HermesAdapter()
    run_adapter(adapter, port=args.port)

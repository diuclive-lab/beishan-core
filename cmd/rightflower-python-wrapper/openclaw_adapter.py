#!/usr/bin/env python3
"""
OpenClaw 右花适配器 — 通过 HTTP API 暴露 OpenClaw 真实能力

继承 rightflower_adapter.RightFlowerAdapter，注册真实能力 handler。
调用 OpenClaw Gateway 的 OpenAI 兼容 API 和 Tool Invoke API。

启动：
  python3 openclaw_adapter.py --port 9533

依赖：
  - OpenClaw Gateway 已启动（port 18789）
  - OPENCLAW_GATEWAY_TOKEN 或 ~/.openclaw/openclaw.json 中读取
"""

import json
import os
import sys
import argparse

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from rightflower_adapter import RightFlowerAdapter, Finding, Result, run_adapter

try:
    import urllib.request
    import urllib.error
except ImportError:
    pass


class OpenClawAdapter(RightFlowerAdapter):
    """OpenClaw 真实能力适配"""

    def __init__(self, gateway_url="http://127.0.0.1:18789", token=""):
        super().__init__(name="openclaw", source_label="openclaw")
        self.gateway_url = gateway_url.rstrip("/")
        self.token = token or self._load_token()

        # 注册能力
        self.register("agent.chat", self.handle_agent_chat, "OpenClaw 对话（OpenAI 兼容 API）")
        self.register("tool.execute", self.handle_tool_execute, "执行 OpenClaw 工具")
        self.register("skills.list", self.handle_skills_list, "列出 OpenClaw 已安装 skills")
        self.register("gateway.status", self.handle_gateway_status, "OpenClaw Gateway 健康状态")

        print(f"[openclaw] Gateway: {self.gateway_url}")
        print(f"[openclaw] Token: {self.token[:12]}...")

    @staticmethod
    def _load_token():
        """从 OpenClaw config 文件读取 token"""
        config_path = os.path.expanduser("~/.openclaw/openclaw.json")
        if os.path.exists(config_path):
            with open(config_path) as f:
                cfg = json.load(f)
            return cfg.get("gateway", {}).get("auth", {}).get("token", "")
        return os.environ.get("OPENCLAW_GATEWAY_TOKEN", "")

    def _api_call(self, path: str, data: dict = None, method: str = "POST") -> dict:
        """调 OpenClaw Gateway API"""
        url = f"{self.gateway_url}{path}"
        headers = {
            "Authorization": f"Bearer {self.token}",
            "Content-Type": "application/json",
        }
        body = json.dumps(data).encode() if data else None
        req = urllib.request.Request(url, data=body, headers=headers, method=method)
        try:
            with urllib.request.urlopen(req, timeout=30) as resp:
                return json.loads(resp.read().decode())
        except urllib.error.HTTPError as e:
            err_body = e.read().decode() if e.fp else ""
            return {"error": f"HTTP {e.code}: {err_body[:200]}"}
        except urllib.error.URLError as e:
            return {"error": f"连接失败: {e.reason}"}
        except Exception as e:
            return {"error": str(e)}

    # ── 能力实现 ─────────────────────────────────

    def handle_agent_chat(self, params: dict) -> Result:
        prompt = params.get("prompt", "")
        if not prompt:
            return Result([Finding("agent.chat", "prompt required")])

        resp = self._api_call("/v1/chat/completions", {
            "model": "openclaw",
            "messages": [{"role": "user", "content": prompt}],
            "max_tokens": params.get("max_tokens", 1024),
        })
        if "error" in resp:
            return Result([Finding("agent.chat", f"API 错误: {resp['error']}", "openclaw")])
        choices = resp.get("choices", [])
        if not choices:
            return Result([Finding("agent.chat", "无响应", "openclaw")])
        content = choices[0].get("message", {}).get("content", "")
        usage = resp.get("usage", {})
        summary = f"响应({len(content)}字)"
        if usage:
            summary += f" token:{usage.get('total_tokens', '?')}"
        return Result([Finding("agent.chat", content, "openclaw")])

    def handle_tool_execute(self, params: dict) -> Result:
        tool_name = params.get("tool", "")
        action = params.get("action", "json")
        args = params.get("args", {})

        if not tool_name:
            return Result([Finding("tool.execute", "tool name required")])

        resp = self._api_call("/tools/invoke", {
            "tool": tool_name,
            "action": action,
            "args": args,
        })
        if "error" in resp:
            return Result([Finding(f"tool.execute:{tool_name}", f"错误: {resp['error']}", "openclaw")])

        result = resp.get("result", resp)
        return Result([Finding(f"tool:{tool_name}", json.dumps(result, ensure_ascii=False)[:500], "openclaw")])

    def handle_skills_list(self, params: dict) -> Result:
        """通过 OpenClaw CLI 获取 skills 列表"""
        import subprocess
        try:
            result = subprocess.run(
                ["openclaw", "skills", "list", "--json"],
                capture_output=True, text=True, timeout=15,
            )
            if result.returncode == 0:
                data = json.loads(result.stdout)
                skills = data if isinstance(data, list) else data.get("skills", [])
                findings = []
                for s in skills[:50]:
                    name = s.get("name", s.get("title", "?"))
                    status = s.get("status", s.get("state", "?"))
                    desc = s.get("description", "")[:80]
                    findings.append(Finding(f"[{status}] {name}: {desc}", "OpenClaw skill", "openclaw"))
                if not findings:
                    findings.append(Finding("skills.list", "无已安装 skills", "openclaw"))
                return Result(findings)
            return Result([Finding("skills.list", f"CLI 错误: {result.stderr[:200]}", "openclaw")])
        except FileNotFoundError:
            return Result([Finding("skills.list", "openclaw CLI 未安装", "openclaw")])
        except subprocess.TimeoutExpired:
            return Result([Finding("skills.list", "CLI 超时", "openclaw")])

    def handle_gateway_status(self, params: dict) -> Result:
        resp = self._api_call("/health", method="GET")
        if "error" in resp:
            status = f"不可达: {resp['error']}"
        else:
            status = json.dumps(resp, ensure_ascii=False)
        return Result([Finding("gateway.status", status, "openclaw")])


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--port", type=int, default=9533)
    parser.add_argument("--gateway-url", default="http://127.0.0.1:18789")
    parser.add_argument("--token", default="")
    args = parser.parse_args()

    adapter = OpenClawAdapter(gateway_url=args.gateway_url, token=args.token)
    run_adapter(adapter, port=args.port)

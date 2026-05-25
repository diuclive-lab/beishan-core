"""
rightflower-adapter — Python 右花接入标准模板

使用方式：
  1. 继承 RightFlowerAdapter 类，实现各 method 对应的 _handle_xxx 方法
  2. 注册能力映射：adapter.register("method.name", handler)
  3. 启动：python rightflower_adapter.py --port 9532

协议：
  POST /dispatch  {"method":"xxx","params":{}}  →  {"result":{"findings":[...]}}
  GET  /health                                   →  {"status":"ok"}
"""

import json
import os
import sys
from http.server import HTTPServer, BaseHTTPRequestHandler
from typing import Any


# ─── 协议数据结构 ──────────────────────────────────

class Finding:
    """右花返回的一条发现"""
    def __init__(self, title: str, summary: str, source: str = "rightflower"):
        self.title = title
        self.summary = summary
        self.verified = False       # 外部来源永不标记为 verified
        self.source = source

    def to_dict(self) -> dict:
        return {
            "title": self.title,
            "summary": self.summary,
            "verified": self.verified,
            "source": self.source,
        }


class Result:
    """右花响应结果"""
    def __init__(self, findings: list[Finding] = None):
        self.findings = findings or []

    def to_dict(self) -> dict:
        return {"findings": [f.to_dict() for f in self.findings]}


# ─── 能力注册 ─────────────────────────────────────

class RightFlowerAdapter:
    """右花适配器基类。继承并注册能力映射。"""

    def __init__(self, name: str = "rightflower", source_label: str = "rightflower"):
        self.name = name
        self.source_label = source_label
        self._handlers = {}         # method → handler 映射
        self._descriptions = {}     # method → 描述

    def register(self, method: str, handler, description: str = ""):
        """注册一个能力。handler 接收 params(dict) 返回 Result。"""
        self._handlers[method] = handler
        self._descriptions[method] = description

    def handle_dispatch(self, body: dict) -> dict:
        """处理 dispatch 请求。返回完整的 response dict。"""
        method = body.get("method", "")
        params = body.get("params", {})

        if not method:
            return {"error": "method required"}

        handler = self._handlers.get(method)
        if not handler:
            methods = ", ".join(self._handlers.keys())
            return {"error": f"unknown method: {method}. available: {methods}"}

        try:
            result = handler(params)
            if isinstance(result, Result):
                return {"result": result.to_dict()}
            return {"result": result}
        except Exception as e:
            return {"error": f"{type(e).__name__}: {e}"}

    def list_methods(self) -> list[dict]:
        """列出所有已注册的能力"""
        return [
            {"method": m, "description": self._descriptions.get(m, "")}
            for m in sorted(self._handlers.keys())
        ]


# ─── HTTP 服务 ────────────────────────────────────

class DispatchHandler(BaseHTTPRequestHandler):
    adapter: RightFlowerAdapter = None

    def do_GET(self):
        if self.path == "/health":
            self.send_json({"adapter": self.adapter.name, "status": "ok"})
        elif self.path == "/methods":
            self.send_json({"methods": self.adapter.list_methods()})
        else:
            self.send_error(404)

    def do_POST(self):
        if self.path != "/dispatch":
            self.send_error(404)
            return

        length = int(self.headers.get("Content-Length", 0))
        body = json.loads(self.rfile.read(length))

        result = self.adapter.handle_dispatch(body)
        self.send_json(result)

    def send_json(self, data: dict, status: int = 200):
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(json.dumps(data, ensure_ascii=False).encode())

    def log_message(self, format, *args):
        print(f"[rightflower] {args[0]} {args[1]}", file=sys.stderr)


def run_adapter(adapter: RightFlowerAdapter, port: int = 9532):
    """启动右花 HTTP 服务"""
    DispatchHandler.adapter = adapter
    server = HTTPServer(("0.0.0.0", port), DispatchHandler)
    print(f"[rightflower] {adapter.name} 启动于 :{port}")
    print(f"[rightflower] 能力: {[m['method'] for m in adapter.list_methods()]}")

    try:
        server.serve_forever()
    except KeyboardInterrupt:
        server.shutdown()

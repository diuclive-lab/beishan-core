#!/usr/bin/env python3
"""
web_render_py — Python 网页渲染插件，通过 glue IPC 协议运行。

协议：stdin 读 dispatch, stdout 写 response, stderr 打日志。
消息格式：JSON 一行一条，末尾换行。

生命周期：
  1. 启动后立即发 register
  2. 循环读 stdin，处理 dispatch
  3. 收到 shutdown 退出
"""

import json
import sys
import traceback

try:
    from playwright.sync_api import sync_playwright
except ImportError:
    playwright_available = False
else:
    playwright_available = True


def log(msg: str):
    """打日志到 stderr，不影响 stdout 协议。"""
    print(f"[web_render_py] {msg}", file=sys.stderr, flush=True)


def send(msg: dict):
    """写一行 JSON 到 stdout。"""
    sys.stdout.write(json.dumps(msg, ensure_ascii=False) + "\n")
    sys.stdout.flush()


def handle_dispatch(msg: dict) -> dict:
    """处理 dispatch 消息，返回 response。"""
    payload = msg.get("payload", {})
    msg_type = msg.get("msg_type", "")

    if msg_type == "web_render":
        return handle_web_render(payload)
    else:
        return {
            "status": "error",
            "error": f"unknown msg_type: {msg_type}",
        }


def handle_web_render(payload: dict) -> dict:
    """Playwright 渲染指定 URL 并提取正文。"""
    url = payload.get("url", "")
    if not url:
        return {"status": "error", "error": "url required"}

    wait_sec = payload.get("wait", 5)
    if not isinstance(wait_sec, (int, float)) or wait_sec < 0:
        wait_sec = 5
    if wait_sec > 30:
        wait_sec = 30

    if not playwright_available:
        return {
            "status": "error",
            "error": "playwright not installed. run: pip install playwright && playwright install chromium",
        }

    try:
        with sync_playwright() as pw:
            browser = pw.chromium.launch(headless=True)
            page = browser.new_page()
            page.goto(url, wait_until="networkidle", timeout=(wait_sec + 15) * 1000)
            page.wait_for_timeout(wait_sec * 1000)
            title = page.title()
            content = page.evaluate("document.body.innerText")
            browser.close()

        if content and len(content) > 100000:
            content = content[:100000] + f"\n... [truncated, total {len(content)} chars]"

        return {
            "status": "ok",
            "payload": {
                "url": url,
                "title": title or "",
                "content": content or "",
                "success": True,
            },
        }
    except Exception as e:
        return {
            "status": "error",
            "error": f"render failed: {e}",
        }


def main():
    # 1. 注册
    send({"type": "register", "name": "web_render_py"})
    log("registered, waiting for dispatch...")

    # 2. 主循环
    for line in sys.stdin:
        line = line.strip()
        if not line:
            continue

        try:
            msg = json.loads(line)
        except json.JSONDecodeError as e:
            log(f"invalid JSON: {e}")
            continue

        msg_type = msg.get("type", "")

        if msg_type == "shutdown":
            log("shutdown received, exiting")
            send({"type": "response", "status": "ok", "error": "shutdown"})
            break

        elif msg_type == "dispatch":
            log(f"dispatch: {msg.get('msg_type', '')} id={msg.get('id', '')}")
            try:
                result = handle_dispatch(msg)
                send({
                    "type": "response",
                    "id": msg.get("id", ""),
                    "status": result.get("status", "error"),
                    "error": result.get("error", ""),
                    "payload": result.get("payload"),
                })
            except Exception as e:
                log(f"dispatch error: {traceback.format_exc()}")
                send({
                    "type": "response",
                    "id": msg.get("id", ""),
                    "status": "error",
                    "error": str(e),
                })

        else:
            log(f"unknown message type: {msg_type}")

    log("exited cleanly")


if __name__ == "__main__":
    main()

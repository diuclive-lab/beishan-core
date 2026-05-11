"""Python 插件标准模板。

   所有 Python 插件都从这个模板开始。
   你只需要修改 handle_dispatch 函数里的业务逻辑。
"""

import sys, json

PLUGIN_NAME = "python_example"  # 改成你的插件名


def handle_dispatch(msg: dict) -> dict:
    """处理 dispatch 消息，返回 response。

        参数:
        msg: {"type":"dispatch","id":"...","sender":"...","msg_type":"...","payload":...}

        返回:
        {"type":"response","id":"...","status":"ok/error","payload":...,"error":"..."}
    """
    msg_type = msg.get("msg_type", "")
    payload = msg.get("payload", {})

    # ─── 在这里写你的业务逻辑 ─────────────────────
    print(f"[{PLUGIN_NAME}] 收到消息: type={msg_type} payload={payload}", file=sys.stderr)
    result = {"echo": payload}
    # ──────────────────────────────────────────

    return {
        "type": "response",
        "id": msg.get("id", ""),
        "status": "ok",
        "payload": result
    }


def main():
    # 启动时注册
    sys.stdout.write(json.dumps({"type": "register", "name": PLUGIN_NAME}) + "\n")
    sys.stdout.flush()

    for line in sys.stdin:
        line = line.strip()
        if not line:
            continue

        try:
            msg = json.loads(line)
        except json.JSONDecodeError:
            continue

        if msg.get("type") == "shutdown":
            break

        if msg.get("type") == "dispatch":
            resp = handle_dispatch(msg)
            sys.stdout.write(json.dumps(resp) + "\n")
            sys.stdout.flush()


if __name__ == "__main__":
    main()

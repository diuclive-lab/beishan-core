"""L3 Python 插件标准模板。

所有 Python 语言 L3 插件都从此模板开始。
由胶水层（L2）spawn 为子进程，通过 stdin/stdout JSON 通信。

协议流程：
1. 启动时向 stdout 写 {"type": "register", "name": "插件名"}
2. 从 stdin 读取 dispatch 消息并处理
3. 处理完成后向 stdout 写 response
4. 收到 shutdown 消息时退出

trace_id / timestamp / retry_count 由胶水层自动注入到 dispatch 消息中，
插件无需自行生成，但在 response 中如需关联可带回。
"""

import sys
import json

PLUGIN_NAME = "l3_echo_python"  # 改成你的插件名，须与 manifest.json 一致


def handle_dispatch(msg: dict) -> dict:
    """处理 dispatch 消息，返回 response。

    参数:
        msg: {"type":"dispatch","id":"...","msg_type":"...","payload":...,"trace_id":"...","timestamp":...}

    返回:
        {"type":"response","id":"...","status":"ok/error","payload":...,"error":"..."}
    """
    msg_type = msg.get("msg_type", "")
    payload = msg.get("payload", "")

    # ─── 业务逻辑（示例：echo） ──────────────────────
    print(f"[{PLUGIN_NAME}] 收到: type={msg_type} payload={payload}", file=sys.stderr)
    result = {"echo": payload, "plugin": PLUGIN_NAME}

    return {
        "type": "response",
        "id": msg.get("id", ""),
        "status": "ok",
        "payload": result,
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
            print(f"[{PLUGIN_NAME}] 收到 shutdown，退出", file=sys.stderr)
            break

        if msg.get("type") == "dispatch":
            resp = handle_dispatch(msg)
            sys.stdout.write(json.dumps(resp) + "\n")
            sys.stdout.flush()


if __name__ == "__main__":
    main()

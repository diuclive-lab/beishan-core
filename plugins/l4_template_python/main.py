"""L4 标准模板（Python 语言）

   L4 的职责：编排 L3 完成多步任务。

   核心模式与 Go 版一致：
   1. 收到复杂任务
   2. 拆分子步骤
   3. 每步调 L3 获取结果
   4. 聚合结果返回

   Python L4 插件通过 IPC 与内核通信。
   子步骤通过 Call() 同步等待 L3 结果。
"""

import sys, json, uuid

PLUGIN_NAME = "l4_research"


def call_plugin(kernel, recipient, msg_type, payload, timeout=30):
    """通过内核调 L3 插件，同步等结果。

       与 Go 版的 kernel.Call() 对应。
       通过 stdin/stdout IPC 实现请求-响应。
    """
    call_id = str(uuid.uuid4())[:8]

    # 构造 dispatch 消息，带上 call_id
    dispatch = {
        "type": "dispatch",
        "id": call_id,
        "recipient": recipient,
        "sender": PLUGIN_NAME,
        "msg_type": msg_type,
        "payload": payload
    }

    # 发送给内核（写到 stdout，由胶水层转发）
    sys.stdout.write(json.dumps(dispatch) + "\n")
    sys.stdout.flush()

    # 等响应（读 stdin，直到拿到匹配 call_id 的响应）
    # 实际场景建议加超时逻辑
    return wait_response(call_id, timeout)


def wait_response(call_id, timeout=30):
    """从 stdin 读取响应，匹配 call_id。"""
    import select

    deadline = timeout
    while deadline > 0:
        # 检查 stdin 是否有数据可读
        ready, _, _ = select.select([sys.stdin], [], [], 0.5)
        if not ready:
            deadline -= 0.5
            continue

        line = sys.stdin.readline().strip()
        if not line:
            continue

        msg = json.loads(line)

        # 跳过非响应消息
        if msg.get("type") != "response":
            continue

        # 匹配 call_id
        if msg.get("id") == call_id:
            return msg

    raise TimeoutError(f"{PLUGIN_NAME} 调用 {call_id} 超时")


def handle_research(topic):
    """做研究：搜索 → 分析 → 保存"""

    print(f"[{PLUGIN_NAME}] 开始研究: {topic}", file=sys.stderr)

    # ─── 步骤 1：搜索 ─────────────────────────
    search_result = call_plugin(
        kernel=None,  # 实际通过 IPC
        recipient="search_plugin",
        msg_type="web_search",
        payload=topic
    )

    print(f"[{PLUGIN_NAME}] 搜索完成", file=sys.stderr)

    # ─── 步骤 2：分析搜索结果 ──────────────────
    # 这里可以再调别的 L3（如 think_plugin 做分析）

    # ─── 步骤 3：保存结果 ─────────────────────
    call_plugin(
        kernel=None,
        recipient="write_plugin",
        msg_type="write_file",
        payload=f"研究结果: {topic}"
    )

    print(f"[{PLUGIN_NAME}] 研究完成: {topic}", file=sys.stderr)

    return {"topic": topic, "status": "done"}


def main():
    # 启动时注册
    register = {"type": "register", "name": PLUGIN_NAME}
    sys.stdout.write(json.dumps(register) + "\n")
    sys.stdout.flush()

    for line in sys.stdin:
        line = line.strip()
        if not line:
            continue

        msg = json.loads(line)

        if msg.get("type") == "shutdown":
            break

        if msg.get("type") == "dispatch":
            msg_type = msg.get("msg_type", "")
            payload = msg.get("payload", {})

            if msg_type == "research":
                result = handle_research(payload)

                resp = {
                    "type": "response",
                    "id": msg.get("id", ""),
                    "status": "ok",
                    "payload": result
                }
                sys.stdout.write(json.dumps(resp) + "\n")
                sys.stdout.flush()


if __name__ == "__main__":
    main()

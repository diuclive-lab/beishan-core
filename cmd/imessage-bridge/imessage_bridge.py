#!/usr/bin/env python3
"""
iMessage Bridge — 通过 AppleScript 连接 iMessage ↔ beishan-core。

工作方式：
  1. 30 秒轮询 Messages.app 的新消息
  2. 未读消息 → POST 到 beishan-core /api/chat
  3. 响应 → AppleScript 回复 iMessage

启动：
  python3 imessage_bridge.py [--port 8013]

注意：
  - 首次启动需要在 系统设置 → 隐私与安全性 → 自动化
    中允许终端控制 Messages.app
  - macOS 14+ 可能需要完全磁盘访问权限
"""

import os
import sys
import json
import time
import subprocess
import urllib.request
import urllib.error

BEISHAN_URL = "http://127.0.0.1:8013/api/chat"
POLL_INTERVAL = 30  # seconds
SCRIPT_NAME = os.path.basename(__file__)

# ── AppleScript 命令 ─────────────────────────────

CHECK_SCRIPT = """
tell application "Messages"
    set recentMsgs to {}
    set chatList to every chat
    repeat with c in chatList
        set msgList to every message of c
        if (count of msgList) > 0 then
            set lastMsg to item -1 of msgList
            set isRead to read status of lastMsg
            if not isRead then
                set senderName to ""
                try
                    set senderName to full name of participant 1 of c
                end try
                set msgId to id of lastMsg
                set msgContent to content of lastMsg
                set end of recentMsgs to {id:msgId, sender:senderName, content:msgContent}
            end if
        end if
    end repeat
    return recentMsgs
end tell
"""

REPLY_SCRIPT = """
on run {targetId, replyText}
    tell application "Messages"
        set targetService to 1st service whose service type = iMessage
        set targetBuddy to buddy targetId of targetService
        send replyText to targetBuddy
    end tell
end run
"""

MARK_READ_SCRIPT = """
on run {msgId}
    tell application "Messages"
        set msg to message id msgId
        set read status of msg to true
    end tell
end run
"""

# ── 工具函数 ───────────────────────────────────

def run_applescript(script):
    """Run AppleScript and return stdout."""
    try:
        r = subprocess.run(["osascript", "-e", script],
                         capture_output=True, text=True, timeout=15)
        return r.stdout.strip(), r.returncode
    except subprocess.TimeoutExpired:
        return "", 1
    except FileNotFoundError:
        return "", 1

def parse_applescript_list(output):
    """Parse AppleScript record list into Python dicts."""
    if not output:
        return []
    results = []
    try:
        # AppleScript returns format: {id:123, sender:"Name", content:"Text"}
        # Multiple records are space-separated
        items = output.split("} {")
        for item in items:
            item = item.strip().strip("{}")
            if not item:
                continue
            parts = item.split(", ")
            entry = {}
            for p in parts:
                if ":" in p:
                    key, val = p.split(":", 1)
                    entry[key.strip()] = val.strip()
            if entry.get("id"):
                results.append(entry)
    except:
        pass
    return results

def send_imessage(contact, text):
    """Send iMessage via AppleScript."""
    script = f"""
    tell application "Messages"
        set targetBuddy to buddy "{contact}" of 1st service whose service type is iMessage
        send "{text}" to targetBuddy
    end tell
    """
    run_applescript(script)

def call_beishan(text):
    """Send message to beishan-core and return response."""
    body = json.dumps({"message": text}).encode()
    req = urllib.request.Request(BEISHAN_URL,
                                 data=body,
                                 headers={"Content-Type": "application/json"},
                                 method="POST")
    try:
        with urllib.request.urlopen(req, timeout=60) as resp:
            data = json.loads(resp.read())
            # Extract the response payload
            payload = data.get("payload", "")
            if isinstance(payload, dict):
                return json.dumps(payload, ensure_ascii=False)
            return str(payload)
    except Exception as e:
        return f"[错误] beishan-core 不可达: {e}"

# ── 主循环 ─────────────────────────────────────

def main():
    print(f"[{SCRIPT_NAME}] iMessage Bridge 启动")
    print(f"[{SCRIPT_NAME}] beishan-core: {BEISHAN_URL}")
    print(f"[{SCRIPT_NAME}] 轮询间隔: {POLL_INTERVAL}s")
    print(f"[{SCRIPT_NAME}] 首次启动请在系统设置中允许终端控制 Messages.app")
    print()

    # Test connection
    try:
        urllib.request.urlopen(BEISHAN_URL.replace("/api/chat", "/health"), timeout=5)
        print("✅ beishan-core 可达")
    except:
        print("⚠️ beishan-core 不可达，桥接将在服务启动后工作")
    print()

    processed = set()

    while True:
        try:
            output, code = run_applescript(CHECK_SCRIPT)
            if code != 0:
                time.sleep(POLL_INTERVAL)
                continue

            msgs = parse_applescript_list(output)
            for msg in msgs:
                msg_id = msg.get("id", "")
                if msg_id in processed:
                    continue
                processed.add(msg_id)

                sender = msg.get("sender", "未知")
                content = msg.get("content", "")

                if not content:
                    continue

                print(f"📨 {sender}: {content[:60]}")
                response = call_beishan(content)
                print(f"💬 → {response[:100]}")
                #send_imessage(sender, response)

            time.sleep(POLL_INTERVAL)

        except KeyboardInterrupt:
            print(f"\n[{SCRIPT_NAME}] 已停止")
            break
        except Exception as e:
            print(f"⚠️ {e}")
            time.sleep(POLL_INTERVAL)

if __name__ == "__main__":
    main()

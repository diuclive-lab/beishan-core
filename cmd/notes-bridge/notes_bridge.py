#!/usr/bin/env python3
"""
备忘录 Bridge — 通过 Apple Notes ↔ beishan-core 对话。

工作方式：
  1. 在 iPhone/Mac 备忘录中创建一条标题为 "beishan" 的笔记
  2. 在笔记中写消息，等待 AI 回复
  3. AI 回复自动追加到同一笔记

使用方法：
  1. iPhone 打开备忘录 → 新建笔记 → 标题写 "beishan"
  2. 笔记内容写你的消息
  3. 等待几秒，AI 回复自动追加到笔记末尾

启动：
  python3 notes_bridge.py
"""

import os
import sys
import json
import time
import subprocess
import urllib.request
import urllib.error

BEISHAN_URL = "http://127.0.0.1:8013/api/chat"
POLL_INTERVAL = 5  # seconds
NOTE_TITLE = "beishan"

# ── AppleScript 命令 ─────────────────────────────

GET_NOTE_SCRIPT = f'''
tell application "Notes"
    set theNotes to every note
    repeat with n in theNotes
        if name of n is "{NOTE_TITLE}" then
            set noteId to id of n
            set noteBody to body of n
            return noteId & "|||" & noteBody
        end if
    end repeat
    return ""
end tell
'''

UPDATE_NOTE_SCRIPT = '''
on run {noteId, newBody}
    tell application "Notes"
        set theNote to note id noteId
        set body of theNote to newBody
    end tell
end run
'''

def run_applescript(script):
    try:
        r = subprocess.run(["osascript", "-e", script],
                         capture_output=True, text=True, timeout=15)
        return r.stdout.strip(), r.returncode
    except:
        return "", 1

def get_note():
    """读取 beishan 笔记的内容和 ID。"""
    out, _ = run_applescript(GET_NOTE_SCRIPT)
    if "|||" in out:
        parts = out.split("|||", 1)
        return parts[0], parts[1]
    return None, None

def update_note(note_id, body):
    """更新笔记内容。"""
    safe_body = body.replace('"', '\\"')
    script = f'''
    tell application "Notes"
        set theNote to note id "{note_id}"
        set body of theNote to "{safe_body}"
    end tell
    '''
    run_applescript(script)

def call_beishan(text):
    """发送消息到 beishan-core 并返回响应。"""
    body = json.dumps({"message": text}).encode()
    req = urllib.request.Request(BEISHAN_URL, data=body,
                                headers={"Content-Type": "application/json"},
                                method="POST")
    try:
        with urllib.request.urlopen(req, timeout=60) as resp:
            data = json.loads(resp.read())
            payload = data.get("payload", "")
            if isinstance(payload, dict):
                return json.dumps(payload, ensure_ascii=False)
            # 清理检索过程等非回复内容
            text = str(payload)
            if "---" in text:
                text = text.split("---")[0]
            return text.strip()
    except Exception as e:
        return f"[错误] {e}"

def strip_html(body):
    """去掉 Notes HTML 标签，提取纯文本。"""
    import re
    text = re.sub(r'<[^>]+>', ' ', body)
    text = re.sub(r'\s+', ' ', text).strip()
    return text

def main():
    print(f"📝 备忘录 Bridge 启动")
    print(f"   请在 iPhone 备忘录中创建标题为「{NOTE_TITLE}」的笔记")
    print(f"   轮询间隔: {POLL_INTERVAL}s")
    print()

    last_content = ""

    while True:
        try:
            note_id, body = get_note()
            if not note_id:
                time.sleep(POLL_INTERVAL * 2)  # 没有笔记就慢点查
                continue

            plain = strip_html(body)
            if plain == last_content:
                time.sleep(POLL_INTERVAL)
                continue

            # 检测到新内容
            new_part = plain[len(last_content):].strip() if plain.startswith(last_content) else plain
            if new_part:
                print(f"📨 收到: {new_part[:60]}")
                response = call_beishan(new_part)
                print(f"💬 回复: {response[:80]}")

                # 将回复追加到笔记
                new_body = body + f"<br><br><b>AI:</b> {response}"
                update_note(note_id, new_body)
                print(f"✅ 已写入备忘录")
                # 重新读取以同步 last_content
                _, body = get_note()
                plain = strip_html(body)
                last_content = plain

            last_content = plain
            time.sleep(POLL_INTERVAL)

        except KeyboardInterrupt:
            print("\n已停止")
            break
        except Exception as e:
            print(f"⚠️ {e}")
            time.sleep(POLL_INTERVAL)

if __name__ == "__main__":
    main()

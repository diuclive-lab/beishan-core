#!/usr/bin/env python3

import json
import os
import platform
import subprocess
import sys


def load_pyautogui():
    try:
        import pyautogui

        pyautogui.FAILSAFE = True
        pyautogui.PAUSE = 0.3
        return pyautogui
    except Exception as exc:
        raise RuntimeError("pyautogui is not available. Install it before enabling desktop tools.") from exc


def mac_scale_factor():
    env_value = os.getenv("MAC_SCALE_FACTOR")
    if env_value:
        try:
            return float(env_value)
        except ValueError:
            pass
    return 2.0 if platform.machine() == "arm64" else 1.0


def click_action(payload):
    pyautogui = load_pyautogui()
    scale = mac_scale_factor()
    raw_x = int(payload["x"])
    raw_y = int(payload["y"])
    logical_x = int(raw_x / scale)
    logical_y = int(raw_y / scale)
    button = str(payload.get("button", "left"))
    pyautogui.moveTo(logical_x, logical_y, duration=0.2)
    pyautogui.click(button=button)
    return {
        "status": "success",
        "summary": f"Clicked desktop position ({logical_x}, {logical_y}).",
        "data": {
            "raw_x": raw_x,
            "raw_y": raw_y,
            "logical_x": logical_x,
            "logical_y": logical_y,
            "button": button,
            "scale_factor": scale,
        },
    }


def type_text_action(payload):
    pyautogui = load_pyautogui()
    text = str(payload.get("text", ""))
    if not text:
        raise ValueError("missing text")
    submit = bool(payload.get("submit", False))
    pyautogui.write(text, interval=0.03)
    if submit:
        pyautogui.press("enter")
    return {
        "status": "success",
        "summary": "Typed text into the focused desktop target.",
        "data": {
            "characters": len(text),
            "submit": submit,
        },
    }


def run_osascript(lines):
    command = ["osascript"]
    for line in lines:
        command.extend(["-e", line])
    completed = subprocess.run(command, capture_output=True, text=True, check=True)
    return completed.stdout.strip()


def split_items(text):
    return [item.strip() for item in str(text or "").split("||") if item.strip()]


def get_window_tree_action(_payload):
    output = run_osascript(
        [
            'tell application "System Events"',
            '  set frontProc to first application process whose frontmost is true',
            '  set frontName to name of frontProc',
            '  set AppleScript\'s text item delimiters to "||"',
            '  set visibleNames to name of (application processes whose background only is false)',
            '  set visibleText to visibleNames as string',
            '  set windowTitles to {}',
            '  repeat with w in windows of frontProc',
            '    try',
            '      set wName to name of w',
            '      if wName is not "" then set end of windowTitles to wName',
            '    end try',
            '  end repeat',
            '  set windowText to windowTitles as string',
            '  return frontName & linefeed & visibleText & linefeed & windowText',
            'end tell',
        ]
    )
    lines = output.splitlines()
    front_name = lines[0].strip() if lines else ""
    visible_apps = split_items(lines[1] if len(lines) > 1 else "")
    front_windows = split_items(lines[2] if len(lines) > 2 else "")
    return {
        "status": "success",
        "summary": f"Captured desktop window tree for {front_name or 'the frontmost application'}.",
        "data": {
            "frontmost_application": front_name,
            "visible_applications": visible_apps,
            "frontmost_windows": front_windows,
        },
    }


def get_menu_bar_tree_action(_payload):
    output = run_osascript(
        [
            'tell application "System Events"',
            '  set frontProc to first application process whose frontmost is true',
            '  set procName to name of frontProc',
            '  set AppleScript\'s text item delimiters to "||"',
            '  set menuNames to {}',
            '  repeat with itemRef in menu bar items of menu bar 1 of frontProc',
            '    try',
            '      set itemName to name of itemRef',
            '      if itemName is not "" then set end of menuNames to itemName',
            '    end try',
            '  end repeat',
            '  return procName & linefeed & (menuNames as string)',
            'end tell',
        ]
    )
    lines = output.splitlines()
    app_name = lines[0].strip() if lines else ""
    menu_items = split_items(lines[1] if len(lines) > 1 else "")
    return {
        "status": "success",
        "summary": f"Captured top-level menu bar items for {app_name or 'the frontmost application'}.",
        "data": {
            "application": app_name,
            "menu_items": menu_items,
        },
    }


def click_menu_item_action(payload):
    identifier = str(payload.get("menu_identifier", "")).strip()
    if not identifier:
        raise ValueError("missing menu_identifier")

    if identifier.isdigit():
        output = run_osascript(
            [
                'tell application "System Events"',
                '  set frontProc to first application process whose frontmost is true',
                f'  set targetIndex to {int(identifier)}',
                '  set targetItem to menu bar item targetIndex of menu bar 1 of frontProc',
                '  set clickedName to name of targetItem',
                '  click targetItem',
                '  return (name of frontProc) & linefeed & clickedName',
                'end tell',
            ]
        )
    else:
        escaped = identifier.replace('"', '\\"')
        output = run_osascript(
            [
                'tell application "System Events"',
                '  set frontProc to first application process whose frontmost is true',
                f'  set targetName to "{escaped}"',
                '  set targetItem to first menu bar item of menu bar 1 of frontProc whose name is targetName',
                '  click targetItem',
                '  return (name of frontProc) & linefeed & targetName',
                'end tell',
            ]
        )

    lines = output.splitlines()
    app_name = lines[0].strip() if lines else ""
    clicked_name = lines[1].strip() if len(lines) > 1 else identifier
    return {
        "status": "success",
        "summary": f"Clicked menu item {clicked_name}.",
        "data": {
            "application": app_name,
            "clicked_menu_item": clicked_name,
            "menu_identifier": identifier,
        },
    }


def main():
    try:
        payload = json.loads(sys.stdin.read().strip() or "{}")
        action = str(payload.get("action") or "").strip()
        if action == "click":
            response = click_action(payload)
        elif action == "type_text":
            response = type_text_action(payload)
        elif action == "get_window_tree":
            response = get_window_tree_action(payload)
        elif action == "get_menu_bar_tree":
            response = get_menu_bar_tree_action(payload)
        elif action == "click_menu_item":
            response = click_menu_item_action(payload)
        else:
            raise ValueError(f"unknown action: {action}")
        sys.stdout.write(json.dumps(response, ensure_ascii=False))
    except Exception as exc:
        sys.stdout.write(json.dumps({"status": "error", "error": str(exc)}, ensure_ascii=False))
        sys.exit(1)


if __name__ == "__main__":
    main()

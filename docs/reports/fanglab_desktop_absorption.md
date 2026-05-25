# FangLab 桌面操作能力吸收记录

> 2026-05-25。吸收工作流 Step 1-6 完成。

## 吸收来源

FangLab (`/Users/dc/Desktop/66/FangLab`)，同宗同源项目。
- `scripts/desktop_actuator.py` — pyautogui 桌面自动化脚本
- `api/http/server.go` — API 路由参考（21 端点）

## 吸收内容

| 能力 | 内化位置 | 状态 |
|------|---------|------|
| 桌面操作：点击 | `internal/tools/desktop.go` → call click_action | ✅ |
| 桌面操作：输入文字 | `internal/tools/desktop.go` → call type_text_action | ✅ |
| 桌面操作：窗口树 | `internal/tools/desktop.go` → call get_window_tree | ✅ |
| 桌面操作：菜单栏 | `internal/tools/desktop.go` → call get_menu_bar_tree | ✅ |
| 桌面操作：点击菜单 | `internal/tools/desktop.go` → call click_menu_item | ✅ |

## 验证

```bash
# get_window_tree → "Captured desktop window tree for Notes."
# get_menu_bar_tree → "Captured top-level menu bar items for Notes."
# tools count: 100 → 101
```

## 广度检查

- kernel/: 零改动 ✅
- plugins/: 零改动（memory_plugin 动态转发）✅
- 安全：操作类型白名单 + Python 子进程隔离 ✅
- 不存在孤岛 ✅

## 缺口（未吸收的 FangLab 能力）

| 未吸收 | 原因 | 补救 |
|--------|------|------|
| 15 个技能 | 本质是 LLM prompt 组合 | 保持协议调用 |
| 9 工具集 | 部分重叠底座已有 | 选择性吸收（P1） |
| 模型舰队 | 冲突底座 provider 体系 | 不吸收 |
| Chat 对话 | 冲突底座 chat pipeline | 不吸收 |

## 参考项目义务

被吸收项目：FangLab (`/Users/dc/Desktop/66/FangLab`)
- 吸收了 desktop_actuator.py 的桌面操作能力
- 内化到 `internal/tools/desktop.go` + `scripts/desktop_actuator.py`
- 未吸收：技能系统、工具集、模型舰队、chat（原因见上）

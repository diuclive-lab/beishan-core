package browser

import (
	"fmt"
	"os"
)

// EngineKind 浏览器引擎类型。
type EngineKind string

const (
	EngineChrome  EngineKind = "chrome"
	EngineServo   EngineKind = "servo"
	EngineUnknown EngineKind = ""
)

// DetectEngine 从环境变量检测要使用的浏览器引擎。
// BEISHAN_BROWSER=chrome|servo，默认 chrome。
func DetectEngine() EngineKind {
	switch os.Getenv("BEISHAN_BROWSER") {
	case "servo":
		return EngineServo
	default:
		return EngineChrome
	}
}

// NewEngine 创建并启动 Detected 的浏览器引擎。
func NewEngine() (Engine, error) {
	return NewEngineKind(DetectEngine())
}

// NewEngineKind 创建并启动指定类型的浏览器引擎。
func NewEngineKind(kind EngineKind) (Engine, error) {
	switch kind {
	case EngineChrome:
		profile := os.Getenv("BEISHAN_CHROME_PROFILE")
		if profile == "" {
			profile = DefaultProfileDir()
		}
		return NewChrome(profile, true)
	case EngineServo:
		return NewServo()
	default:
		return nil, fmt.Errorf("未知浏览器引擎: %s", kind)
	}
}

// DefaultProfileDir 返回默认的持久 Chrome profile 路径。
func DefaultProfileDir() string {
	home, _ := os.UserHomeDir()
	return home + "/.hermes/chrome_profile"
}


// NewAgentSession 创建隔离的 Agent 浏览器会话（临时 profile，用完销毁）。
// 参考 OWL StoragePartition ephemeral logged-out context 设计。
func NewAgentSession() (Engine, error) {
	return NewChromeWithConfig(ChromeConfig{
		UserDataDir: "",
		Headless:    true,
		Incognito:   true,
	})
}

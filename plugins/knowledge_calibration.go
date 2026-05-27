package plugins

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"beishan/internal/tools"
)

// CalibrationEvent 记录每次知识分类及用户对结果的反应。
// 持久化到 JSONL，用于精度计算和自动化门槛判定。
type CalibrationEvent struct {
	Timestamp   int64   `json:"ts"`
	ContentType string  `json:"type"`
	Confidence  float64 `json:"conf"`
	Title       string  `json:"title"`
	Action      string  `json:"action"` // confirmed | rejected | corrected | expired | auto_confirmed
	SessionID   string  `json:"sid,omitempty"`
}

// autoThresholds 各内容类型触发自动入库的精度阈值（滑动窗口精度 ≥ 阈值才开启）
var autoThresholds = map[string]float64{
	"work_record":           0.80,
	"decision":              0.90,
	"lesson":                0.85,
	"fact":                  0.85,
	"link":                  0.80,
	"open_source_community": 0.80,
	"image":                 0.85,
}

const (
	calibMinSamples = 10 // 最小样本量（低于此值不开启自动模式）
	calibWindow     = 20 // 滑动窗口大小（只看最近 N 条）
)

var calibMu sync.Mutex

func calibrationPath() string {
	return filepath.Join(tools.MemoryDir, "knowledge_calibration.jsonl")
}

// AppendCalibEvent 追加一条分类事件到 JSONL 文件。
func AppendCalibEvent(ev CalibrationEvent) {
	calibMu.Lock()
	defer calibMu.Unlock()
	f, err := os.OpenFile(calibrationPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	line, _ := json.Marshal(ev)
	f.Write(append(line, '\n'))
}

// loadRecentTypeEvents 读取指定 content_type 最近 N 条已完成事件（非 pending）。
func loadRecentTypeEvents(contentType string, limit int) []CalibrationEvent {
	calibMu.Lock()
	defer calibMu.Unlock()
	f, err := os.Open(calibrationPath())
	if err != nil {
		return nil
	}
	defer f.Close()

	var all []CalibrationEvent
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var ev CalibrationEvent
		if json.Unmarshal(scanner.Bytes(), &ev) == nil && ev.ContentType == contentType {
			all = append(all, ev)
		}
	}
	if len(all) > limit {
		all = all[len(all)-limit:]
	}
	return all
}

// ComputePrecision 计算指定类型在滑动窗口内的精度和有效样本数。
// 精度 = 正面信号（confirmed + auto_confirmed）/ 总信号数
func ComputePrecision(contentType string) (precision float64, samples int) {
	events := loadRecentTypeEvents(contentType, calibWindow)
	confirmed := 0
	for _, ev := range events {
		switch ev.Action {
		case "confirmed", "auto_confirmed":
			confirmed++
		}
	}
	samples = len(events)
	if samples == 0 {
		return 0, 0
	}
	return float64(confirmed) / float64(samples), samples
}

// IsAutoMode 判断指定类型是否已达到自动入库精度阈值。
// 滑动窗口精度天然处理退化：精度下跌自动退出自动模式，无需额外状态。
func IsAutoMode(contentType string) bool {
	precision, samples := ComputePrecision(contentType)
	if samples < calibMinSamples {
		return false
	}
	threshold, ok := autoThresholds[contentType]
	if !ok {
		threshold = 0.85
	}
	return precision >= threshold
}

// CalibStatus 返回各类型的自动化状态（供调试/查询用）。
func CalibStatus() map[string]interface{} {
	result := make(map[string]interface{})
	for ct, threshold := range autoThresholds {
		precision, samples := ComputePrecision(ct)
		auto := samples >= calibMinSamples && precision >= threshold
		result[ct] = map[string]interface{}{
			"precision": fmt.Sprintf("%.0f%%", precision*100),
			"samples":   samples,
			"threshold": fmt.Sprintf("%.0f%%", threshold*100),
			"auto":      auto,
		}
	}
	return result
}

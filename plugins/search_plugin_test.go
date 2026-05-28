package plugins

import (
	"testing"
)

func TestStripDateFromQuery_ChineseDate(t *testing.T) {
	// "国内金价 2026年5月28日" → 应去掉日期
	input := "国内金价 2026年5月28日"
	got := stripDateFromQuery(input)
	expected := "国内金价" // TrimSpace 去掉末尾空格
	if got != expected {
		t.Errorf("期望 %q, 实际 %q", expected, got)
	}
}

func TestStripDateFromQuery_NoDate(t *testing.T) {
	// "上海黄金交易所 Au9999" → 不含日期，应返回原始字符串
	input := "上海黄金交易所 Au9999"
	got := stripDateFromQuery(input)
	if got != input {
		t.Errorf("无日期时应返回原始字符串: %q", got)
	}
}

func TestStripDateFromQuery_TodayKeywords(t *testing.T) {
	// "今日金价" → 去掉"今日"，返回"金价"
	// 去掉"今日"后搜索"金价"可匹配更多商家折扣信息，不被日期限制
	input := "今日金价"
	got := stripDateFromQuery(input)
	expected := "金价"
	if got != expected {
		t.Errorf("期望 %q, 实际 %q", expected, got)
	}
	// 去掉"昨天""今日"这类时间词的意义：搜索结果时效性由搜索引擎保证，
	// 日期限定词反而可能导致零结果或低相关性——去掉后结果更多样，
	// 再通过 LLM 判断哪些是真正相关的。
}

func TestStripDateFromQuery_DateDash(t *testing.T) {
	// "2026-05-28 黄金行情" → 去掉日期部分
	input := "2026-05-28 黄金行情"
	got := stripDateFromQuery(input)
	expected := "黄金行情" // TrimSpace 去掉前导空格
	if got != expected {
		t.Errorf("期望 %q, 实际 %q", expected, got)
	}
}

func TestStripDateFromQuery_DateYearMonth(t *testing.T) {
	// "黄金行情 2026年5月" → 去掉年月
	input := "黄金行情 2026年5月"
	got := stripDateFromQuery(input)
	expected := "黄金行情" // TrimSpace 去掉末尾空格
	if got != expected {
		t.Errorf("期望 %q, 实际 %q", expected, got)
	}
}

func TestStripDateFromQuery_OnlyDate(t *testing.T) {
	// 只有日期本身 → 返回原始（stripped 为空）
	input := "2026年5月28日"
	got := stripDateFromQuery(input)
	if got != input {
		t.Errorf("仅日期时应返回原始: %q", got)
	}
}

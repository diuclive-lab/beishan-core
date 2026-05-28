package tools

import (
	"testing"
)

func TestUngroundedNumbersWarn_FabricatedPrice(t *testing.T) {
	// 合成输出包含"738元/克"，但工具来源中没有"738"——应返回非空警告
	synth := "周大福今日金价738元/克，较昨日上涨0.5%"
	source := "上海黄金交易所 Au9999 收盘价: 586 元/克"
	warn := UngroundedNumbersWarn(synth, source)
	if warn == "" {
		t.Error("应检测到未接地数字 738元/克")
	}
	t.Logf("警告: %s", warn)
}

func TestUngroundedNumbersWarn_PriceMatchesSource(t *testing.T) {
	// 合成输出包含"586元/克"，工具来源也包含"586"——应返回空（无警告）
	synth := "Au9999 收盘价 586元/克，较昨日下跌"
	source := "黄金价格: 586元/克"
	warn := UngroundedNumbersWarn(synth, source)
	if warn != "" {
		t.Errorf("不应警告，数字586在来源中: %s", warn)
	}
}

func TestUngroundedNumbersWarn_YearNotTriggered(t *testing.T) {
	// 合成输出包含"2026年"——不应触发警告（年份不是经济数字）
	synth := "截至2026年5月，黄金市场整体上行"
	source := "黄金市场分析报告"
	warn := UngroundedNumbersWarn(synth, source)
	if warn != "" {
		t.Errorf("年份不应触发警告: %s", warn)
	}
}

func TestUngroundedNumbersWarn_NoNumbers(t *testing.T) {
	// 合成输出没有任何带单位数字——返回空
	synth := "根据搜索结果，黄金价格在近期有所波动。"
	source := ""
	warn := UngroundedNumbersWarn(synth, source)
	if warn != "" {
		t.Errorf("无数字时不应有警告: %s", warn)
	}
}

func TestUngroundedNumbersWarn_PercentageThreshold(t *testing.T) {
	// 百分比数字应被检测（% 在单位列表中）
	synth := "涨幅达到12.5%，创年内新高"
	source := "涨幅 10%"
	warn := UngroundedNumbersWarn(synth, source)
	if warn == "" {
		t.Error("应检测到未接地的百分比 12.5%")
	}
}

func TestUngroundedNumbersWarn_BareNumberInSource(t *testing.T) {
	// 工具来源包含纯数字匹配（不带单位），也应视为接地
	synth := "成交量为35000手"
	source := "成交量 35000"
	warn := UngroundedNumbersWarn(synth, source)
	if warn != "" {
		t.Errorf("数字35000在来源中存在（纯数字匹配）: %s", warn)
	}
}

func TestUngroundedNumbersWarn_CommaSeparated(t *testing.T) {
	// 数字含逗号分隔符
	synth := "市值为25,000亿元"
	source := "市值 25000 亿元"
	warn := UngroundedNumbersWarn(synth, source)
	if warn != "" {
		t.Errorf("25000和25,000应视为等同: %s", warn)
	}
}

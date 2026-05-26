package tools

import (
	"testing"
)

func TestRadixTreeInsertAndMatch(t *testing.T) {
	tr := NewRadixTree()
	tr.Insert("business_weather", "weather_api")
	tr.Insert("business_translate", "translate")
	tr.Insert("business_currency", "currency_converter")
	tr.Insert("business_stock", "stock_api")

	tests := []struct {
		input   string
		wantVal interface{}
		wantLen int
	}{
		{"business_weather", "weather_api", 16},
		{"business_translate", "translate", 18},
		{"business_currency", "currency_converter", 17},
		{"business_stock", "stock_api", 14},
		{"nonexistent", nil, 0},
		{"", nil, 0},
	}

	for _, tc := range tests {
		val, matchLen := tr.Match(tc.input)
		if val != tc.wantVal {
			t.Errorf("Match(%q) = %v, want %v", tc.input, val, tc.wantVal)
		}
		if matchLen != tc.wantLen {
			t.Errorf("Match(%q) matchLen = %d, want %d", tc.input, matchLen, tc.wantLen)
		}
	}
}

func TestRadixTreePrefixMatch(t *testing.T) {
	tr := NewRadixTree()
	tr.Insert("business_", "business_prefix")
	tr.Insert("business_weather", "weather_api")

	val, matchLen := tr.Match("business_weather")
	if val == nil {
		t.Fatal("expected match for business_weather")
	}
	if val != "weather_api" {
		t.Fatalf("expected weather_api, got %v", val)
	}
	if matchLen != 16 {
		t.Fatalf("expected matchLen=16 (business_weather), got %d", matchLen)
	}

	val, matchLen = tr.Match("business_translate")
	if val == nil {
		t.Fatal("expected match for business_translate")
	}
	if val != "business_prefix" {
		t.Fatalf("expected business_prefix, got %v", val)
	}
	if matchLen != 9 {
		t.Fatalf("expected matchLen=9 (business_), got %d", matchLen)
	}
}

func TestRadixTreeKeywordMatch(t *testing.T) {
	tr := NewRadixTree()
	tr.Insert("天气", "weather")
	tr.Insert("翻译", "translate")
	tr.Insert("股价", "stock")

	tests := []struct {
		input       string
		wantVal     interface{}
		wantKeyword string
	}{
		{"北京今天天气怎么样", "weather", "天气"},
		{"把hello翻译成中文", "translate", "翻译"},
		{"苹果公司股价是多少", "stock", "股价"},
	}

	for _, tc := range tests {
		val, kw := tr.MatchKeyword(tc.input)
		if val != tc.wantVal {
			t.Errorf("MatchKeyword(%q) = %v, want %v", tc.input, val, tc.wantVal)
		}
		if kw != tc.wantKeyword {
			t.Errorf("MatchKeyword(%q) keyword = %q, want %q", tc.input, kw, tc.wantKeyword)
		}
	}
}

func TestRadixTreeDuplicateInsert(t *testing.T) {
	tr := NewRadixTree()
	tr.Insert("test", 1)
	if tr.Size() != 1 {
		t.Fatalf("expected size 1, got %d", tr.Size())
	}
	if tr.Insert("test", 2) {
		t.Fatal("duplicate insert should return false")
	}
	if tr.Size() != 1 {
		t.Fatalf("expected size 1 after duplicate, got %d", tr.Size())
	}
}

func TestRadixTreeCommonPrefixSplit(t *testing.T) {
	tr := NewRadixTree()
	tr.Insert("foobar", 1)
	tr.Insert("foobaz", 2)

	val, matchLen := tr.Match("foobar")
	if val != 1 || matchLen != 6 {
		t.Fatalf("foobar: got %v/%d, want 1/6", val, matchLen)
	}

	val, matchLen = tr.Match("foobaz")
	if val != 2 || matchLen != 6 {
		t.Fatalf("foobaz: got %v/%d, want 2/6", val, matchLen)
	}
}

func TestRadixTreeNestedPrefix(t *testing.T) {
	tr := NewRadixTree()
	tr.Insert("a", 1)
	tr.Insert("ab", 2)
	tr.Insert("abc", 3)

	val, matchLen := tr.Match("a")
	if val != 1 || matchLen != 1 {
		t.Fatalf("a: got %v/%d", val, matchLen)
	}
	val, matchLen = tr.Match("abcd")
	if val != 3 || matchLen != 3 {
		t.Fatalf("abcd should match longest prefix (abc): got %v/%d", val, matchLen)
	}
}

func TestRadixTreeNilSafety(t *testing.T) {
	var tr *RadixTree
	val, matchLen := tr.Match("test")
	if val != nil || matchLen != 0 {
		t.Fatal("nil tree should return nil/0")
	}
}

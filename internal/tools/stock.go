package tools

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

/* ─── stock 行情查询 L3 工具 ─────────────────────

   用新浪财经免费 API 查询 A 股实时行情。
   零 API key，零依赖，只做 HTTP GET。
*/

type StockQuote struct {
	Code      string  `json:"code"`       // sh600519
	Name      string  `json:"name"`       // 贵州茅台
	Open      float64 `json:"open"`        // 今开
	PrevClose float64 `json:"prev_close"`  // 昨收
	Price     float64 `json:"price"`       // 当前价
	High      float64 `json:"high"`        // 最高
	Low       float64 `json:"low"`         // 最低
	Volume    int64   `json:"volume"`      // 成交量（手）
	Amount    float64 `json:"amount"`      // 成交额（万）
	Change    float64 `json:"change"`      // 涨跌额
	ChangePct string  `json:"change_pct"`  // 涨跌幅 %
}

// StockCode 格式化股票代码：600519 → sh600519, 000001 → sz000001
func StockCode(code string) string {
	code = strings.TrimSpace(code)
	if strings.HasPrefix(code, "sh") || strings.HasPrefix(code, "sz") {
		return code
	}
	if strings.HasPrefix(code, "6") || code == "5" {
		return "sh" + code
	}
	return "sz" + code
}

func fetchStockQuote(code string) (*StockQuote, error) {
	fullCode := StockCode(code)
	url := fmt.Sprintf("https://hq.sinajs.cn/list=%s", fullCode)

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Referer", "https://finance.sina.com.cn")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	raw := strings.TrimSpace(string(body))

	// 解析新浪格式：var hq_str_sh600519="名称,open,prev_close,price,high,low,...";
	start := strings.Index(raw, `"`)
	end := strings.LastIndex(raw, `"`)
	if start < 0 || end <= start {
		return nil, fmt.Errorf("无法解析行情数据: %s", raw[:min(len(raw), 100)])
	}
	fields := strings.Split(raw[start+1:end], ",")
	if len(fields) < 10 {
		return nil, fmt.Errorf("行情字段不足: %d 个", len(fields))
	}

	q := &StockQuote{
		Code: fullCode,
		Name: fields[0],
	}
	fmt.Sscanf(fields[1], "%f", &q.Open)
	fmt.Sscanf(fields[2], "%f", &q.PrevClose)
	fmt.Sscanf(fields[3], "%f", &q.Price)
	fmt.Sscanf(fields[4], "%f", &q.High)
	fmt.Sscanf(fields[5], "%f", &q.Low)
	fmt.Sscanf(fields[6], "%f", &q.Price) // 买一价 ≈ 当前价
	fmt.Sscanf(fields[8], "%d", &q.Volume)
	fmt.Sscanf(fields[9], "%f", &q.Amount)

	q.Change = q.Price - q.PrevClose
	if q.PrevClose > 0 {
		pct := q.Change / q.PrevClose * 100
		q.ChangePct = fmt.Sprintf("%.2f%%", pct)
	}

	return q, nil
}

func StockQuoteHandler(args map[string]interface{}) *ToolResult {
	code, _ := args["code"].(string)
	if code == "" {
		return errorResult("code 不能为空，如 600519 或 sh600519")
	}

	quote, err := fetchStockQuote(code)
	if err != nil {
		return errorResult(fmt.Sprintf("查询失败: %v", err))
	}

	b, _ := json.MarshalIndent(quote, "", "  ")
	return successResult(string(b))
}

func StockMultiQuoteHandler(args map[string]interface{}) *ToolResult {
	codesRaw, ok := args["codes"].([]interface{})
	if !ok || len(codesRaw) == 0 {
		return errorResult("codes 不能为空，如 [\"600519\",\"000001\"]")
	}

	var results []StockQuote
	var errors []string
	for _, c := range codesRaw {
		code, ok := c.(string)
		if !ok {
			continue
		}
		q, err := fetchStockQuote(code)
		if err != nil {
			errors = append(errors, fmt.Sprintf("%s: %v", code, err))
		} else {
			results = append(results, *q)
		}
	}

	output := map[string]interface{}{
		"results": results,
		"count":   len(results),
	}
	if len(errors) > 0 {
		output["errors"] = errors
	}
	b, _ := json.MarshalIndent(output, "", "  ")
	return successResult(string(b))
}

func registerStockTools() {
	Register("stock_quote", "查询 A 股实时行情（免费，零 API key）。支持 sh/sz 前缀或自动识别。",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"code"},
			"properties": map[string]interface{}{
				"code": stringParam("股票代码，如 600519 或 sh600519 或 sz000001"),
			},
		},
		StockQuoteHandler,
	)

	Register("stock_multi_quote", "批量查询多只 A 股实时行情。",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"codes"},
			"properties": map[string]interface{}{
				"codes": map[string]interface{}{
					"type":        "array",
					"description": "股票代码列表，如 [\"600519\",\"000001\"]",
					"items":       map[string]interface{}{"type": "string"},
				},
			},
		},
		StockMultiQuoteHandler,
	)
}

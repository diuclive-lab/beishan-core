// Package currency provides currency conversion using open.er-api.com (no API key).
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ── Types ────────────────────────────────────────────────────────────────────

// Request is the input for currency conversion.
type Request struct {
	From   string  `json:"from"`
	To     string  `json:"to"`
	Amount float64 `json:"amount"`
}

// Result is the output of currency conversion.
type Result struct {
	From      string  `json:"from"`
	To        string  `json:"to"`
	Amount    float64 `json:"amount"`
	Rate      float64 `json:"rate"`
	Converted float64 `json:"converted"`
}

// Provider abstracts currency conversion.
type Provider interface {
	Convert(ctx context.Context, req Request) (Result, error)
}

// ── Tool ─────────────────────────────────────────────────────────────────────

// Tool provides currency conversion.
type CurrencyTool struct {
	Provider Provider
}

// New creates a currency tool with the HTTP provider.
func NewCurrencyTool() *CurrencyTool {
	return &CurrencyTool{Provider: &HTTPProvider{}}
}

func (t *CurrencyTool) Name() string { return "currency" }

func (t *CurrencyTool) Run(ctx context.Context, args map[string]any) (string, error) {
	from := getArg(args, "from", "source", "base")
	to := getArg(args, "to", "target", "dest")
	amount := 1.0
	if v, ok := args["amount"]; ok {
		switch n := v.(type) {
		case float64:
			amount = n
		case int:
			amount = float64(n)
		}
	}
	if from == "" || to == "" {
		return "", fmt.Errorf("currency: from and to are required")
	}

	result, err := t.Provider.Convert(ctx, Request{From: from, To: to, Amount: amount})
	if err != nil {
		return "", fmt.Errorf("currency: %w", err)
	}

	return fmt.Sprintf("%.2f %s = %.2f %s (汇率: %.4f)",
		result.Amount, result.From, result.Converted, result.To, result.Rate), nil
}

func getArg(args map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := args[k]; ok {
			s, _ := v.(string)
			if s != "" {
				return s
			}
		}
	}
	return ""
}

// ── HTTP Provider ────────────────────────────────────────────────────────────

type exchangeRateResponse struct {
	Base  string             `json:"base_code"`
	Rates map[string]float64 `json:"rates"`
}

// HTTPProvider fetches exchange rates from open.er-api.com.
type HTTPProvider struct{}

func (p *HTTPProvider) Convert(ctx context.Context, req Request) (Result, error) {
	apiURL := fmt.Sprintf("https://open.er-api.com/v6/latest/%s", req.From)
	client := &http.Client{Timeout: 10 * time.Second}
	httpReq, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return Result{}, fmt.Errorf("build request: %w", err)
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return Result{}, fmt.Errorf("exchange rate API: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var result exchangeRateResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return Result{}, fmt.Errorf("parse response: %w", err)
	}

	rate, ok := result.Rates[req.To]
	if !ok {
		return Result{}, fmt.Errorf("currency not found: %s", req.To)
	}

	return Result{
		From: req.From, To: req.To, Amount: req.Amount,
		Rate: rate, Converted: req.Amount * rate,
	}, nil
}

func registerCurrencyTool() {
	Register("weather", "查询天气（Open-Meteo，免费）",
		map[string]interface{}{},
		func(args map[string]interface{}) *ToolResult {
			t := NewWeatherTool()
			result, err := t.Run(context.TODO(), args)
			if err != nil {
				return errorResult(err.Error())
			}
			return successResult(result)
		},
	)
}

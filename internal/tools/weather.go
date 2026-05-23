// Package weather provides a weather query tool.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const openMeteoURL = "https://api.open-meteo.com/v1/forecast"

type WeatherTool struct{}

func NewWeatherTool() *WeatherTool { return &WeatherTool{} }

func (t *WeatherTool) Name() string { return "weather" }

func (t *WeatherTool) Run(ctx context.Context, args map[string]any) (string, error) {
	city := ""
	for _, key := range []string{"city", "location", "城市"} {
		if v, ok := args[key]; ok {
			city, _ = v.(string)
			break
		}
	}
	if city == "" {
		return "", fmt.Errorf("city is required")
	}

	// Use Open-Meteo (no API key needed) with geocoding
	lat, lon, err := geocode(ctx, city)
	if err != nil {
		lat, lon = 39.9, 116.4 // default Beijing
	}

	u := fmt.Sprintf("%s?latitude=%.2f&longitude=%.2f&current_weather=true&timezone=auto", openMeteoURL, lat, lon)
	resp, err := http.Get(u)
	if err != nil {
		return "", fmt.Errorf("weather api: %w", err)
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	var result struct {
		CurrentWeather struct {
			Temperature float64 `json:"temperature"`
		} `json:"current_weather"`
	}
	json.Unmarshal(data, &result)

	return fmt.Sprintf("%s 当前温度: %.1f°C", city, result.CurrentWeather.Temperature), nil
}

type geocodeResult struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

func geocode(ctx context.Context, city string) (float64, float64, error) {
	u := fmt.Sprintf("https://geocoding-api.open-meteo.com/v1/search?name=%s&count=1", url.QueryEscape(city))
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return 0, 0, err
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	var result struct {
		Results []geocodeResult `json:"results"`
	}
	if err := json.Unmarshal(data, &result); err != nil || len(result.Results) == 0 {
		return 0, 0, fmt.Errorf("city not found")
	}
	return result.Results[0].Lat, result.Results[0].Lon, nil
}

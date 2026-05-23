// Package clarify defines standard data types for clarification interactions.
package clarify

import "time"

// Request is what the system produces when it needs clarification.
type Request struct {
	NeedsClarify bool     `json:"needs_clarify"`
	Question     string   `json:"question"`
	Candidates   []string `json:"candidates"`
	Confidence   float64  `json:"confidence"`
	Evidence     []string `json:"evidence,omitempty"`
}

// Response records what the user answered.
type Response struct {
	Input      string   `json:"input"`
	Selected   string   `json:"selected"`
	Candidates []string `json:"candidates"`
	Timestamp  string   `json:"timestamp"`
}

// NewResponse creates a Response with the current timestamp.
func NewResponse(input, selected string, candidates []string) Response {
	return Response{
		Input:      input,
		Selected:   selected,
		Candidates: candidates,
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
	}
}

// BuildQuestion formats a clarifying question for the user.
func BuildQuestion(input string, candidates []string, evidence []string) string {
	if len(candidates) == 0 {
		return "请更具体地描述您想做什么"
	}
	question := "您是想要"
	for i, c := range candidates {
		if i > 0 {
			question += "、"
		}
		question += c
	}
	question += "？"
	return question
}

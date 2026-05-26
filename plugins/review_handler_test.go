package plugins

import (
	"testing"
	"time"
)

func TestIsBatchConfirm(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"确认 1,2", true},
		{"确认 1,2,3", true},
		{"确认 review_abc 1,2", true},
		{"确认", false},
		{"确认 ", false},
		{"hello", false},
		{"", false},
	}
	for _, tc := range tests {
		got := isBatchConfirm(tc.input)
		if got != tc.want {
			t.Errorf("isBatchConfirm(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestParseIndices(t *testing.T) {
	tests := []struct {
		input    string
		want     []int
		wantErr  bool
	}{
		{"1,2,3", []int{1, 2, 3}, false},
		{"1", []int{1}, false},
		{"1, 2, 3", []int{1, 2, 3}, false},
		{"", nil, true},
		{"0", nil, true},
		{"abc", nil, true},
	}
	for _, tc := range tests {
		got, err := parseIndices(tc.input)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parseIndices(%q) expected error", tc.input)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseIndices(%q) unexpected error: %v", tc.input, err)
			continue
		}
		if len(got) != len(tc.want) {
			t.Errorf("parseIndices(%q) = %v, want %v", tc.input, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("parseIndices(%q)[%d] = %d, want %d", tc.input, i, got[i], tc.want[i])
			}
		}
	}
}

func TestParseBatchConfirmWithID(t *testing.T) {
	tests := []struct {
		input       string
		wantReview  string
		wantIndices []int
		wantErr     bool
	}{
		{"确认 1,2", "", []int{1, 2}, false},
		{"确认 review_abc 1,2", "review_abc", []int{1, 2}, false},
		{"确认 1, 2, 3", "", []int{1, 2, 3}, false},
		{"确认 ", "", nil, true},
		{"hello", "", nil, true},
	}
	for _, tc := range tests {
		reviewID, indices, err := parseBatchConfirmWithID(tc.input)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parseBatchConfirmWithID(%q) expected error", tc.input)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseBatchConfirmWithID(%q) unexpected error: %v", tc.input, err)
			continue
		}
		if reviewID != tc.wantReview {
			t.Errorf("parseBatchConfirmWithID(%q) reviewID = %q, want %q", tc.input, reviewID, tc.wantReview)
		}
		if len(indices) != len(tc.wantIndices) {
			t.Errorf("parseBatchConfirmWithID(%q) indices = %v, want %v", tc.input, indices, tc.wantIndices)
			continue
		}
		for i := range indices {
			if indices[i] != tc.wantIndices[i] {
				t.Errorf("parseBatchConfirmWithID(%q) indices[%d] = %d, want %d", tc.input, i, indices[i], tc.wantIndices[i])
			}
		}
	}
}

func TestIsReviewTrigger(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"审查一下", true},
		{"知识审查", true},
		{"knowledge review", true},
		{"hello", false},
		{"", false},
	}
	for _, tc := range tests {
		got := isReviewTrigger(tc.input)
		if got != tc.want {
			t.Errorf("isReviewTrigger(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestIsListReviews(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"待审查报告", true},
		{"审查队列", true},
		{"review queue", true},
		{"hello", false},
	}
	for _, tc := range tests {
		got := isListReviews(tc.input)
		if got != tc.want {
			t.Errorf("isListReviews(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestIsConfirmAll(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"确认全部", true},
		{"全部入库", true},
		{"confirm all", true},
		{"hello", false},
	}
	for _, tc := range tests {
		got := isConfirmAll(tc.input)
		if got != tc.want {
			t.Errorf("isConfirmAll(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestIsSkipAll(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"跳过", true},
		{"清理审查", true},
		{"skip", true},
		{"hello", false},
	}
	for _, tc := range tests {
		got := isSkipAll(tc.input)
		if got != tc.want {
			t.Errorf("isSkipAll(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestIsConfirmReply(t *testing.T) {
	sessionID := "test_session_confirm"
	// No pending — should return false
	if isConfirmReply(sessionID, "确认") {
		t.Error("isConfirmReply should be false without pending")
	}
	// Create pending
	pr := createPendingRemember(sessionID, "test_title", "test_summary")
	if pr == nil {
		t.Fatal("createPendingRemember failed")
	}
	// Now should match
	if !isConfirmReply(sessionID, "确认") {
		t.Error("isConfirmReply should be true with pending")
	}
	if !isConfirmReply(sessionID, "是") {
		t.Error("isConfirmReply should match '是'")
	}
	if !isConfirmReply(sessionID, "yes") {
		t.Error("isConfirmReply should match 'yes'")
	}
	if isConfirmReply(sessionID, "no") {
		t.Error("isConfirmReply should not match 'no'")
	}
}

func TestIsForceSaveReply(t *testing.T) {
	sessionID := "test_session_force"
	if isForceSaveReply(sessionID, "强制记录") {
		t.Error("isForceSaveReply should be false without pending")
	}
	createPendingRemember(sessionID, "test", "test")
	if !isForceSaveReply(sessionID, "强制记录") {
		t.Error("isForceSaveReply should match '强制记录'")
	}
	if !isForceSaveReply(sessionID, "是的，强制记录") {
		t.Error("isForceSaveReply should match '是的，强制记录'")
	}
}

func TestIsMergeReply(t *testing.T) {
	sessionID := "test_session_merge"
	if isMergeReply(sessionID, "合并") {
		t.Error("isMergeReply should be false without pending")
	}
	createPendingRemember(sessionID, "test", "test")
	if !isMergeReply(sessionID, "合并") {
		t.Error("isMergeReply should match '合并'")
	}
	if !isMergeReply(sessionID, "确认合并") {
		t.Error("isMergeReply should match '确认合并'")
	}
}

func TestPendingRememberExpiry(t *testing.T) {
	sessionID := "test_session_expiry"
	createPendingRemember(sessionID, "test", "test")
	// Manually expire it
	s := sessionManager.Get(sessionID)
	if s.Pending != nil {
		s.Pending.ExpiresAt = time.Now().Unix() - 1
	}
	if isConfirmReply(sessionID, "确认") {
		t.Error("isConfirmReply should be false after expiry")
	}
}

func TestCleanupExpiredPending(t *testing.T) {
	sessionID := "test_session_cleanup"
	createPendingRemember(sessionID, "test", "test")
	s := sessionManager.Get(sessionID)
	if s.Pending != nil {
		s.Pending.ExpiresAt = time.Now().Unix() - 1
	}
	cleanupExpiredPending()
	s2 := sessionManager.Get(sessionID)
	if s2.Pending != nil {
		t.Error("expired pending should be cleaned up")
	}
}

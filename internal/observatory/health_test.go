package observatory

import "testing"

// TestRecordDegradation 证明降级登记机制：记录后 Degradations() 可见，且返回的是副本
// （调用方修改不污染内部态——呼应 R2 的测试隔离卫生）。
func TestRecordDegradation(t *testing.T) {
	// 重置（同包可直接访问私有态，避免跨测试污染）
	degMu.Lock()
	degradations = nil
	degMu.Unlock()

	if len(Degradations()) != 0 {
		t.Fatal("重置后应无降级")
	}

	RecordDegradation("comp-a", "boom reason")
	got := Degradations()
	if len(got) != 1 {
		t.Fatalf("应有 1 条降级，实得 %d", len(got))
	}
	if got[0].Component != "comp-a" || got[0].Reason != "boom reason" {
		t.Fatalf("降级内容不符: %+v", got[0])
	}

	// Degradations 返回副本：改返回值不影响内部态
	got[0].Component = "mutated"
	if Degradations()[0].Component != "comp-a" {
		t.Fatal("Degradations 应返回副本，调用方修改不应影响内部态")
	}
}

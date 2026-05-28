package glue

import (
	"encoding/json"
	"strconv"
	"sync"
	"testing"
)

// ─── IPC 通道压力测试 ─────────────────────────────────

// TestResponseChannelContention 模拟多个 goroutine 同时发送到同一个 responseCh。
func TestResponseChannelContention(t *testing.T) {
	ch := make(chan *ProtocolMessage, 100)
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				msg := &ProtocolMessage{
					Type:    "response",
					ID:      strconv.Itoa(id*1000 + j),
					Payload: json.RawMessage(`{"result":"ok"}`),
				}
				select {
				case ch <- msg:
				default:
				}
			}
		}(i)
	}

	done := make(chan struct{})
	var received int
	go func() {
		for range ch {
			received++
			if received >= 500 {
				break
			}
		}
		close(done)
	}()

	wg.Wait()
	close(ch)
	<-done

	if received < 100 {
		t.Errorf("预期至少 100 条，实际 %d 条", received)
	}
}

// TestChannelBlockRecovery 模拟 channel 读/写阻塞后恢复。
func TestChannelBlockRecovery(t *testing.T) {
	ch := make(chan *ProtocolMessage)

	consumerDone := make(chan struct{})
	go func() {
		for i := 0; i < 5; i++ {
			<-ch
		}
		close(consumerDone)
	}()

	for i := 0; i < 5; i++ {
		ch <- &ProtocolMessage{Type: "response", ID: "block_test"}
	}

	<-consumerDone
}

// ─── 协议消息边界测试 ─────────────────────────────────

// TestProtocolMessageLargePayload 测试超大 payload 的序列化/反序列化。
func TestProtocolMessageLargePayload(t *testing.T) {
	large := make([]byte, 100000)
	for i := range large {
		large[i] = byte('A' + (i % 26))
	}
	payload := json.RawMessage(`"` + string(large) + `"`)

	msg := ProtocolMessage{
		Type:    "dispatch",
		ID:      "large_payload",
		Payload: payload,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("大 payload 序列化失败: %v", err)
	}

	var decoded ProtocolMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("大 payload 反序列化失败: %v", err)
	}

	if decoded.Type != "dispatch" {
		t.Errorf("type 损坏: %q", decoded.Type)
	}
	if len(decoded.Payload) < 50000 {
		t.Errorf("payload 截断: 期望>=50000, 实际=%d", len(decoded.Payload))
	}
}

// TestProtocolMessageSpecialChars 测试包含特殊字符的 payload。
func TestProtocolMessageSpecialChars(t *testing.T) {
	inputs := []string{
		`{"data":"hello\nworld"}`,
		`{"data":"tab\there"}`,
		`{"data":"unicode: 中文"}`,
		`{"data":"nested: {\"inner\":\"value\"}"}`,
		`{"data":""}`,
		`{"data":null}`,
	}

	for _, input := range inputs {
		msg := ProtocolMessage{Type: "response", Payload: json.RawMessage(input)}
		data, err := json.Marshal(msg)
		if err != nil {
			t.Fatalf("特殊字符序列化失败: %v (input=%q)", err, input)
		}
		var decoded ProtocolMessage
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("特殊字符反序列化失败: %v (input=%q)", err, input)
		}
	}
}

// TestProtocolMessageEdgeCases 测试极端边界情况。
func TestProtocolMessageEdgeCases(t *testing.T) {
	edgeCases := []struct {
		name string
		data string
	}{
		{"空 payload", `{"type":"response","payload":null}`},
		{"缺失 type", `{"id":"no_type"}`},
		{"空 type", `{"type":""}`},
		{"缺失 id", `{"type":"response"}`},
	}

	for _, tc := range edgeCases {
		t.Run(tc.name, func(t *testing.T) {
			var msg ProtocolMessage
			err := json.Unmarshal([]byte(tc.data), &msg)
			if err != nil {
				t.Fatalf("反序列化失败: %v", err)
			}
			_, err = json.Marshal(msg)
			if err != nil {
				t.Fatalf("再序列化失败: %v", err)
			}
		})
	}
}

// ─── 并发状态访问测试 ─────────────────────────────────

// TestConcurrentRightFlowerAccess 测试并发右花注册和状态查询。
func TestConcurrentRightFlowerAccess(t *testing.T) {
	gl, err := newTestGlue()
	if err != nil {
		t.Skipf("无法创建测试实例: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			gl.RegisterRightFlower(
				"test_flower_"+strconv.Itoa(id),
				"http://localhost:"+strconv.Itoa(9000+id)+"/health",
			)
			_ = gl.RightFlowerStatus()
		}(i)
	}
	wg.Wait()

	status := gl.RightFlowerStatus()
	t.Logf("并发注册后右花数量: %d", len(status))
	// 在无网络环境中右花健康检测可能失败，但不应 panic 或死锁
}

// TestConcurrentProcStatus 测试并发进程状态读/写。
func TestConcurrentProcStatus(t *testing.T) {
	gl, err := newTestGlue()
	if err != nil {
		t.Skipf("无法创建测试实例: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = gl.ProcStatus()
			}
		}()
	}
	wg.Wait()
	// 不应 panic 或死锁
}

// newTestGlue 创建一个测试用的 GlueLayer 实例。
func newTestGlue() (*GlueLayer, error) {
	// GlueLayer 需要 kernel 实例，但 ProcStatus/RegisterRightFlower/RightFlowerStatus
	// 只访问 gl.mu 保护的内部字段，不依赖 kernel。
	gl := &GlueLayer{}
	gl.procs = make(map[string]*proc)
	gl.sidecars = make(map[string]*sidecar)
	gl.rightFlowers = make(map[string]string)
	return gl, nil
}

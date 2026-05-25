package channels

import (
	"errors"
	"testing"
)

type testChannel struct {
	name string
}

func (c *testChannel) Name() string       { return c.name }
func (c *testChannel) Send(ChannelMessage) error { return nil }
func (c *testChannel) Connect() error             { return nil }
func (c *testChannel) Close() error               { return nil }

type failChannel struct {
	name string
}

func (c *failChannel) Name() string                    { return c.name }
func (c *failChannel) Send(ChannelMessage) error       { return errors.New("send failed") }
func (c *failChannel) Connect() error                  { return nil }
func (c *failChannel) Close() error                    { return nil }

func TestRegisterAndSend(t *testing.T) {
	m := &Manager{channels: make(map[string]Channel)}
	m.Register(&testChannel{name: "test"})

	names := m.List()
	if len(names) != 1 || names[0] != "test" {
		t.Fatalf("expected [test], got %v", names)
	}

	err := m.Send(ChannelMessage{Channel: "test", Body: "hello"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRegisterDuplicate(t *testing.T) {
	m := &Manager{channels: make(map[string]Channel)}
	m.Register(&testChannel{name: "dup"})
	err := m.Register(&testChannel{name: "dup"})
	if err == nil {
		t.Fatal("expected error for duplicate registration")
	}
}

func TestSendUnknownChannel(t *testing.T) {
	m := &Manager{channels: make(map[string]Channel)}
	err := m.Send(ChannelMessage{Channel: "unknown", Body: "hi"})
	if err == nil {
		t.Fatal("expected error for unknown channel")
	}
}

func TestSendAll(t *testing.T) {
	m := &Manager{channels: make(map[string]Channel)}
	m.Register(&testChannel{name: "ok"})
	m.Register(&failChannel{name: "fail"})

	errs := m.SendAll(ChannelMessage{Body: "broadcast"})
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errs))
	}
}

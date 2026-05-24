package bus

import (
	"testing"
	"time"
)

func TestPublishSubscribe(t *testing.T) {
	b := New()
	received := make(chan string, 1)
	b.Subscribe("test.topic", func(e Event) { received <- e.Data.(string) })
	b.Publish("test.topic", "hello")
	select {
	case msg := <-received:
		if msg != "hello" { t.Fatalf("expected hello, got %q", msg) }
	case <-time.After(time.Second): t.Fatal("timeout")
	}
}

func TestPublishNoSubscriber(t *testing.T) { b := New(); b.Publish("x", "y") }
func TestMultipleSubscribers(t *testing.T) {
	b := New(); count := make(chan int, 3)
	for i := 0; i < 3; i++ { b.Subscribe("m", func(e Event) { count <- 1 }) }
	b.Publish("m", "d"); r := 0
	for i := 0; i < 3; i++ { select { case <-count: r++; case <-time.After(time.Second): break } }
	if r != 3 { t.Fatalf("got %d", r) }
}

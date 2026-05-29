package bus

import (
	"fmt"
	"sync"
	"time"

	"beishan/internal/observatory"
)

type Event struct {
	Topic     string
	Data      interface{}
	Timestamp time.Time
	Source    string
}

type Handler func(Event)

type Bus struct {
	mu       sync.RWMutex
	handlers map[string][]Handler
}

func New() *Bus {
	return &Bus{handlers: make(map[string][]Handler)}
}

func (b *Bus) Subscribe(topic string, handler Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers[topic] = append(b.handlers[topic], handler)
}

func (b *Bus) Publish(topic string, data interface{}) {
	b.mu.RLock()
	handlers := b.handlers[topic]
	b.mu.RUnlock()
	evt := Event{Topic: topic, Data: data, Timestamp: time.Now(), Source: fmt.Sprintf("bus:%s", topic)}
	for _, h := range handlers {
		observatory.SafeGo("bus."+topic, func() { h(evt) })
	}
}

var DefaultBus = New()

package events

import (
	"sync"
	"time"

	svcevents "github.com/AGOrcha/dot-agents/internal/service/events"
)

// AttachBus subscribes the broker to the given topics on an R3 event bus
// and fans every received event out to the broker's SSE subscribers,
// running the store-cache eviction hook exactly as for local publishes.
//
// Per the t04 rescope (§2A coherence), this binds ONLY to the D4.1
// events.EventBus INTERFACE — never the concrete *events.Bus — and
// assumes only the D4.2 G1–G4 floor, so a config-selected backend
// (spec D4.4) can replace the builtin without touching this broker.
// Bridged events keep the source bus timestamp (normalized to the
// schema's whole-second UTC form).
//
// The returned detach func unsubscribes every topic and is idempotent;
// Close also runs it. Attaching to a closed broker returns ErrClosed.
// Errors from bus.Subscribe (e.g. a closed bus) abort the attach and
// unwind any subscriptions already made.
func (b *Broker) AttachBus(bus svcevents.EventBus, topics ...string) (func(), error) {
	streams, unsubs, err := subscribeAll(bus, topics)
	if err != nil {
		return nil, err
	}
	detach := func() {
		for _, unsub := range unsubs {
			unsub()
		}
	}

	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		detach()
		return nil, ErrClosed
	}
	b.detaches = append(b.detaches, detach)
	b.wg.Add(len(streams))
	b.mu.Unlock()

	for _, stream := range streams {
		go b.forward(stream)
	}
	var once sync.Once
	return func() { once.Do(detach) }, nil
}

// subscribeAll opens one bus stream per topic, unwinding on any error so
// a partial attach never leaks subscriptions.
func subscribeAll(bus svcevents.EventBus, topics []string) ([]<-chan svcevents.Event, []func(), error) {
	streams := make([]<-chan svcevents.Event, 0, len(topics))
	unsubs := make([]func(), 0, len(topics))
	for _, topic := range topics {
		stream, unsub, err := bus.Subscribe(topic)
		if err != nil {
			for _, undo := range unsubs {
				undo()
			}
			return nil, nil, err
		}
		streams = append(streams, stream)
		unsubs = append(unsubs, unsub)
	}
	return streams, unsubs, nil
}

// forward drains one bus subscription into the broker fan-out until the
// stream closes (detach, bus unsubscribe, or bus Close). A closed broker
// makes dispatch a no-op, so a late-closing stream is harmless.
func (b *Broker) forward(stream <-chan svcevents.Event) {
	defer b.wg.Done()
	for busEvent := range stream {
		b.evict(busEvent.Payload)
		b.dispatch(busEvent.Topic, busEvent.Payload, busEvent.Timestamp.UTC().Truncate(time.Second))
	}
}

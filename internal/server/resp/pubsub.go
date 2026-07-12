package resp

import "sync"

// Message is a pub/sub payload delivered to a subscriber. Pattern is empty
// for a direct channel subscription delivery, and set to the matching
// pattern for a PSUBSCRIBE delivery (so the forwarder can choose between
// emitting a "message" or "pmessage" push reply).
type Message struct {
	Channel string
	Pattern string
	Payload []byte
}

// PubSub is a minimal in-process publish/subscribe broker: it only routes
// messages between connections held by this one server process (there is no
// cluster/replication fan-out in Phase 1).
type PubSub struct {
	mu       sync.Mutex
	channels map[string]map[*ClientState]struct{}
	patterns map[string]map[*ClientState]struct{}
}

// NewPubSub builds an empty broker.
func NewPubSub() *PubSub {
	return &PubSub{
		channels: make(map[string]map[*ClientState]struct{}),
		patterns: make(map[string]map[*ClientState]struct{}),
	}
}

// Subscribe adds cs as a direct subscriber of channel.
func (p *PubSub) Subscribe(cs *ClientState, channel string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.channels[channel] == nil {
		p.channels[channel] = make(map[*ClientState]struct{})
	}
	p.channels[channel][cs] = struct{}{}
}

// Unsubscribe removes cs from channel's direct subscriber set.
func (p *PubSub) Unsubscribe(cs *ClientState, channel string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if subs, ok := p.channels[channel]; ok {
		delete(subs, cs)
		if len(subs) == 0 {
			delete(p.channels, channel)
		}
	}
}

// SubscribePattern adds cs as a pattern subscriber.
func (p *PubSub) SubscribePattern(cs *ClientState, pattern string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.patterns[pattern] == nil {
		p.patterns[pattern] = make(map[*ClientState]struct{})
	}
	p.patterns[pattern][cs] = struct{}{}
}

// UnsubscribePattern removes cs from pattern's subscriber set.
func (p *PubSub) UnsubscribePattern(cs *ClientState, pattern string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if subs, ok := p.patterns[pattern]; ok {
		delete(subs, cs)
		if len(subs) == 0 {
			delete(p.patterns, pattern)
		}
	}
}

// UnsubscribeAll removes cs from every channel and pattern it is subscribed
// to. Called when a connection closes.
func (p *PubSub) UnsubscribeAll(cs *ClientState) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for ch, subs := range p.channels {
		delete(subs, cs)
		if len(subs) == 0 {
			delete(p.channels, ch)
		}
	}
	for pat, subs := range p.patterns {
		delete(subs, cs)
		if len(subs) == 0 {
			delete(p.patterns, pat)
		}
	}
}

type delivery struct {
	cs      *ClientState
	pattern string
}

// Publish delivers payload to every subscriber of channel (direct or via a
// matching pattern) and returns the number of subscribers it was delivered
// to. Delivery to a slow subscriber (full buffer) is dropped rather than
// blocking the publisher, matching the spirit of Redis's fire-and-forget
// pub/sub (no delivery guarantee/backpressure).
func (p *PubSub) Publish(channel string, payload []byte) int {
	p.mu.Lock()
	var targets []delivery
	for cs := range p.channels[channel] {
		targets = append(targets, delivery{cs: cs})
	}
	for pat, subs := range p.patterns {
		if globMatch(pat, channel) {
			for cs := range subs {
				targets = append(targets, delivery{cs: cs, pattern: pat})
			}
		}
	}
	p.mu.Unlock()

	delivered := 0
	for _, d := range targets {
		if d.cs.subCh == nil {
			continue
		}
		msg := Message{Channel: channel, Pattern: d.pattern, Payload: payload}
		select {
		case d.cs.subCh <- msg:
			delivered++
		default:
		}
	}
	return delivered
}

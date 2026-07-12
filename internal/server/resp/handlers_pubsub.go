package resp

// RegisterPubSub adds SUBSCRIBE, UNSUBSCRIBE, PUBLISH, and PSUBSCRIBE to r.
func RegisterPubSub(r *Registry) {
	r.Register(&Command{Name: "SUBSCRIBE", MinArgs: 2, MaxArgs: -1, NoQueue: true, Handler: handleSubscribe})
	r.Register(&Command{Name: "UNSUBSCRIBE", MinArgs: 1, MaxArgs: -1, NoQueue: true, Handler: handleUnsubscribe})
	r.Register(&Command{Name: "PUBLISH", MinArgs: 3, MaxArgs: 3, Handler: handlePublish})
	r.Register(&Command{Name: "PSUBSCRIBE", MinArgs: 2, MaxArgs: -1, NoQueue: true, Handler: handlePSubscribe})
}

func ensureSubscriberState(cs *ClientState) {
	if cs.subCh != nil {
		return
	}
	cs.subCh = make(chan Message, 64)
	cs.Subscriptions = make(map[string]struct{})
	cs.PSubscriptions = make(map[string]struct{})
	startSubscriberForwarder(cs)
}

// startSubscriberForwarder runs for the lifetime of the connection once it
// has subscribed to at least one channel/pattern, forwarding published
// messages onto the wire. It exits when cs.subCh is closed (connection
// teardown, see cleanupSubscriptions) or a write fails.
func startSubscriberForwarder(cs *ClientState) {
	go func() {
		for msg := range cs.subCh {
			var rep Reply
			if msg.Pattern != "" {
				rep = Array(BulkString("pmessage"), BulkString(msg.Pattern), BulkString(msg.Channel), Bulk(msg.Payload))
			} else {
				rep = Array(BulkString("message"), BulkString(msg.Channel), Bulk(msg.Payload))
			}
			if err := cs.writeReply(rep); err != nil {
				return
			}
			if err := cs.flush(); err != nil {
				return
			}
		}
	}()
}

// cleanupSubscriptions is called once per connection on teardown (see
// conn.go's defer) to unregister from the broker and stop the forwarder
// goroutine.
func cleanupSubscriptions(cs *ClientState) {
	if cs.subCh == nil {
		return
	}
	cs.Deps.PubSub.UnsubscribeAll(cs)
	close(cs.subCh)
}

func handleSubscribe(cs *ClientState, args []string) Reply {
	ensureSubscriberState(cs)
	channels := args[1:]
	return func(w *Writer) error {
		for _, ch := range channels {
			cs.Subscriptions[ch] = struct{}{}
			cs.Deps.PubSub.Subscribe(cs, ch)
			count := len(cs.Subscriptions) + len(cs.PSubscriptions)
			rep := Array(BulkString("subscribe"), BulkString(ch), Int(int64(count)))
			if err := rep(w); err != nil {
				return err
			}
		}
		return nil
	}
}

func handlePSubscribe(cs *ClientState, args []string) Reply {
	ensureSubscriberState(cs)
	patterns := args[1:]
	return func(w *Writer) error {
		for _, pat := range patterns {
			cs.PSubscriptions[pat] = struct{}{}
			cs.Deps.PubSub.SubscribePattern(cs, pat)
			count := len(cs.Subscriptions) + len(cs.PSubscriptions)
			rep := Array(BulkString("psubscribe"), BulkString(pat), Int(int64(count)))
			if err := rep(w); err != nil {
				return err
			}
		}
		return nil
	}
}

func handleUnsubscribe(cs *ClientState, args []string) Reply {
	channels := args[1:]
	if len(channels) == 0 {
		for ch := range cs.Subscriptions {
			channels = append(channels, ch)
		}
	}
	return func(w *Writer) error {
		if len(channels) == 0 {
			count := len(cs.Subscriptions) + len(cs.PSubscriptions)
			rep := Array(BulkString("unsubscribe"), NullBulk(), Int(int64(count)))
			return rep(w)
		}
		for _, ch := range channels {
			delete(cs.Subscriptions, ch)
			if cs.subCh != nil {
				cs.Deps.PubSub.Unsubscribe(cs, ch)
			}
			count := len(cs.Subscriptions) + len(cs.PSubscriptions)
			rep := Array(BulkString("unsubscribe"), BulkString(ch), Int(int64(count)))
			if err := rep(w); err != nil {
				return err
			}
		}
		return nil
	}
}

func handlePublish(cs *ClientState, args []string) Reply {
	channel := args[1]
	payload := []byte(args[2])
	n := cs.Deps.PubSub.Publish(channel, payload)
	return Int(int64(n))
}

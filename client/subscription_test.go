package client

import (
	"errors"
	"testing"

	"github.com/go-theft-craft/headless-minecraft/event"
)

func TestSubscriberReceivesOnlySelectedDomains(t *testing.T) {
	t.Parallel()

	var f fanout
	sub, err := f.subscribe(event.DomainSession, 4)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	f.publish([]event.Event{
		event.Connecting{Address: "a"},
		event.PacketReceived{Packet: "keep_alive"},
	})

	got := <-sub.C()
	if got.Name() != event.NameSessionConnecting {
		t.Fatalf("received %q, want connecting", got.Name())
	}

	select {
	case extra := <-sub.C():
		t.Fatalf("received unselected event %q", extra.Name())
	default:
	}
}

func TestSubscriberSelectsSeveralDomainsAtOnce(t *testing.T) {
	t.Parallel()

	var f fanout
	sub, _ := f.subscribe(event.DomainSession|event.DomainRaw, 4)

	f.publish([]event.Event{
		event.Connecting{Address: "a"},
		event.PacketReceived{Packet: "keep_alive"},
	})

	if got := len(sub.C()); got != 2 {
		t.Fatalf("subscriber holds %d events, want both", got)
	}
}

func TestSlowSubscriberOverflowsAndCloses(t *testing.T) {
	t.Parallel()

	var f fanout
	sub, _ := f.subscribe(event.DomainSession, 1)

	f.publish([]event.Event{event.Connecting{Address: "a"}})
	f.publish([]event.Event{event.Closed{}})

	// Drain what fitted, then the channel must be closed.
	<-sub.C()
	if _, open := <-sub.C(); open {
		t.Fatal("channel stayed open after overflow")
	}
	if !errors.Is(sub.Err(), ErrOverflow) {
		t.Fatalf("Err is %v, want ErrOverflow", sub.Err())
	}
}

func TestOverflowDoesNotStallOtherSubscribers(t *testing.T) {
	t.Parallel()

	var f fanout
	slow, _ := f.subscribe(event.DomainSession, 1)
	fast, _ := f.subscribe(event.DomainSession, 16)

	for range 8 {
		f.publish([]event.Event{event.Connecting{Address: "a"}})
	}

	if len(fast.C()) != 8 {
		t.Fatalf("fast subscriber holds %d events, want 8", len(fast.C()))
	}
	<-slow.C()
	if _, open := <-slow.C(); open {
		t.Fatal("slow subscriber survived overflow")
	}
}

func TestOverflowedSubscriptionIsForgotten(t *testing.T) {
	t.Parallel()

	var f fanout
	sub, _ := f.subscribe(event.DomainSession, 1)

	f.publish([]event.Event{event.Connecting{Address: "a"}})
	f.publish([]event.Event{event.Closed{}})

	// A dropped subscription that stayed in the slice would be delivered to
	// again, which sends on a closed channel and panics.
	f.publish([]event.Event{event.Closed{}})

	if got := len(f.subs); got != 0 {
		t.Errorf("fanout still holds %d subscriptions, want 0", got)
	}
	_ = sub
}

func TestCloseIsIdempotentAndRaceFree(t *testing.T) {
	t.Parallel()

	var f fanout
	sub, _ := f.subscribe(event.DomainSession, 4)

	if err := sub.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := sub.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if err := sub.Err(); err != nil {
		t.Errorf("a closed subscription reports %v, want nil", err)
	}

	// Publishing to a closed subscription must not panic on a closed channel.
	f.publish([]event.Event{event.Connecting{Address: "a"}})
}

func TestCloseAllClosesEverySubscription(t *testing.T) {
	t.Parallel()

	var f fanout
	first, _ := f.subscribe(event.DomainSession, 4)
	second, _ := f.subscribe(event.DomainRaw, 4)

	f.closeAll()

	if _, open := <-first.C(); open {
		t.Error("first subscription stayed open")
	}
	if _, open := <-second.C(); open {
		t.Error("second subscription stayed open")
	}
}

func TestSubscribeRejectsANonPositiveBuffer(t *testing.T) {
	t.Parallel()

	var f fanout
	if _, err := f.subscribe(event.DomainSession, 0); err == nil {
		t.Fatal("subscribe accepted a zero buffer")
	}
}

func TestSubscribeRejectsAnEmptySelector(t *testing.T) {
	t.Parallel()

	var f fanout
	if _, err := f.subscribe(0, 4); err == nil {
		t.Fatal("subscribe accepted a selector that matches nothing")
	}
}

func TestPublishingNothingIsNotAnEvent(t *testing.T) {
	t.Parallel()

	var f fanout
	sub, _ := f.subscribe(event.DomainSession, 4)

	f.publish(nil)

	if got := len(sub.C()); got != 0 {
		t.Errorf("subscriber received %d events from an empty batch", got)
	}
}

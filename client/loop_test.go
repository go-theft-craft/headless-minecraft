package client

import (
	"context"
	"errors"
	"io"
	"testing"

	protocol "github.com/go-theft-craft/minecraft-protocol"

	"github.com/go-theft-craft/headless-minecraft/event"
	"github.com/go-theft-craft/headless-minecraft/version"
	"github.com/go-theft-craft/headless-minecraft/world"
)

// sliceReceiver replays packets and then returns io.EOF.
type sliceReceiver struct {
	packets []protocol.Packet
	at      int
}

func (r *sliceReceiver) Receive(context.Context) (protocol.Packet, error) {
	if r.at >= len(r.packets) {
		return protocol.Packet{}, io.EOF
	}
	p := r.packets[r.at]
	r.at++

	return p, nil
}

// blockingReceiver blocks until its context ends.
type blockingReceiver struct{}

func (blockingReceiver) Receive(ctx context.Context) (protocol.Packet, error) {
	<-ctx.Done()

	return protocol.Packet{}, ctx.Err()
}

// recordingSender captures readiness replies.
type recordingSender struct{ sent []protocol.Packet }

func (s *recordingSender) Write(_ context.Context, p protocol.Packet) error {
	s.sent = append(s.sent, p)

	return nil
}

// failingSender stands in for a connection that died mid-reply.
type failingSender struct{ err error }

func (s failingSender) Write(context.Context, protocol.Packet) error { return s.err }

// countingReadiness reports ready on the batch containing "position" and
// asks for one reply packet.
type countingReadiness struct {
	ready bool
	calls int
}

func (r *countingReadiness) Observe(b version.Batch) (version.ReadyState, []protocol.Packet, error) {
	r.calls++
	for _, p := range b.Packets {
		if p.Name == "position" && !r.ready {
			r.ready = true

			return version.ReadyState{Ready: true, EntityID: 7}, []protocol.Packet{
				{Name: "teleport_confirm", State: "play"},
			}, nil
		}
	}

	return version.ReadyState{}, nil, nil
}

// failingReadiness always errors, standing in for a relative spawn.
type failingReadiness struct{ err error }

func (r failingReadiness) Observe(version.Batch) (version.ReadyState, []protocol.Packet, error) {
	return version.ReadyState{}, nil, r.err
}

// recordingHandler counts the packets dispatched to it.
type recordingHandler struct{ names []string }

func (h *recordingHandler) Handle(_ context.Context, p protocol.Packet) error {
	h.names = append(h.names, p.Name)

	return nil
}

// failingHandler stands in for a handler that could not make sense of a
// packet it was registered for.
type failingHandler struct{ err error }

func (h failingHandler) Handle(context.Context, protocol.Packet) error { return h.err }

// harness builds a loop over a fixed packet script.
type harness struct {
	client    *Client
	receiver  *sliceReceiver
	sender    *recordingSender
	batcher   *version.Batcher
	collector *event.Collector
	outbox    *version.Outbox
	ready     chan version.ReadyState
}

// newHarness wires a loop with no router registrations, so every packet
// dispatches to nothing and only the batcher, readiness rule, and fan-out are
// under test. Pass an empty delimiter for an unbundled protocol.
func newHarness(t *testing.T, delimiter string, limit int, names ...string) *harness {
	t.Helper()

	packets := make([]protocol.Packet, 0, len(names))
	for _, name := range names {
		packets = append(packets, protocol.Packet{
			Name: name, State: "play", Direction: protocol.DirectionClientbound,
		})
	}

	batcher, err := version.NewBatcher(delimiter, limit)
	if err != nil {
		t.Fatalf("NewBatcher: %v", err)
	}

	return &harness{
		client:    &Client{done: make(chan struct{})},
		receiver:  &sliceReceiver{packets: packets},
		sender:    &recordingSender{},
		batcher:   batcher,
		collector: new(event.Collector),
		outbox:    new(version.Outbox),
		ready:     make(chan version.ReadyState, 1),
	}
}

// run drives the loop with no router. dispatcher is nil, which the loop must
// tolerate, because a client with no registered handlers is a valid client.
func (h *harness) run(ctx context.Context, rule version.ReadinessRule) error {
	return h.client.runLoop(
		ctx, h.receiver, h.sender, nil, h.batcher, h.collector, h.outbox, rule, h.ready,
	)
}

// received collects the raw packet events a subscription saw.
func received(sub *Subscription) ([]string, []bool) {
	var names []string
	var bundled []bool
	for e := range sub.C() {
		packet, ok := e.(event.PacketReceived)
		if !ok {
			continue
		}
		names = append(names, packet.Packet)
		bundled = append(bundled, packet.Bundled)
	}

	return names, bundled
}

func TestBundledPacketsPublishTogether(t *testing.T) {
	t.Parallel()

	// Two packets inside one bundle must reach a subscriber as one
	// uninterrupted run, with the unbundled packet that follows arriving
	// only after both.
	h := newHarness(t, "bundle", 16,
		"bundle", "spawn_entity", "entity_metadata", "bundle", "keep_alive")

	sub, err := h.client.events.subscribe(event.DomainRaw, 16)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	if err := h.run(t.Context(), &countingReadiness{}); err != nil {
		t.Fatalf("runLoop: %v", err)
	}
	_ = sub.Close()

	names, bundled := received(sub)

	want := []string{"spawn_entity", "entity_metadata", "keep_alive"}
	if len(names) != len(want) {
		t.Fatalf("got %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("got %v, want %v", names, want)
		}
	}
	if !bundled[0] || !bundled[1] {
		t.Error("packets inside the bundle are not marked bundled")
	}
	if bundled[2] {
		t.Error("the packet after the bundle is marked bundled")
	}
}

func TestUnterminatedBundleFailsTheLoop(t *testing.T) {
	t.Parallel()

	h := newHarness(t, "bundle", 16, "bundle", "spawn_entity")

	err := h.run(t.Context(), &countingReadiness{})
	if !errors.Is(err, version.ErrBundleUnterminated) {
		t.Fatalf("got %v, want ErrBundleUnterminated", err)
	}
}

func TestOversizeBundleFailsTheLoop(t *testing.T) {
	t.Parallel()

	h := newHarness(t, "bundle", 2, "bundle", "a", "b", "c")

	err := h.run(t.Context(), &countingReadiness{})
	if !errors.Is(err, version.ErrBundleTooLarge) {
		t.Fatalf("got %v, want ErrBundleTooLarge", err)
	}
}

func TestLoopReturnsNilOnCleanEOF(t *testing.T) {
	t.Parallel()

	h := newHarness(t, "", 16, "keep_alive", "chat")

	if err := h.run(t.Context(), &countingReadiness{}); err != nil {
		t.Fatalf("clean EOF returned %v, want nil", err)
	}
}

func TestLoopReturnsContextErrorOnCancellation(t *testing.T) {
	t.Parallel()

	h := newHarness(t, "", 16)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	// A receiver that blocks until the context ends, so cancellation is
	// observed mid-read rather than after EOF.
	err := h.client.runLoop(
		ctx, blockingReceiver{}, h.sender, nil, h.batcher, h.collector, h.outbox,
		&countingReadiness{}, h.ready,
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
}

func TestReadinessReplyIsWrittenBeforeReadyIsSignalled(t *testing.T) {
	t.Parallel()

	h := newHarness(t, "", 16, "login", "position")

	sub, _ := h.client.events.subscribe(event.DomainSession, 16)
	rule := &countingReadiness{}

	if err := h.run(t.Context(), rule); err != nil {
		t.Fatalf("runLoop: %v", err)
	}
	_ = sub.Close()

	if len(h.sender.sent) != 1 || h.sender.sent[0].Name != "teleport_confirm" {
		t.Fatalf("sent %v, want exactly one teleport_confirm", h.sender.sent)
	}

	var sawReady bool
	for e := range sub.C() {
		if e.Name() != event.SessionReady {
			continue
		}
		if sawReady {
			t.Fatal("Ready was published more than once")
		}
		sawReady = true
	}
	if !sawReady {
		t.Fatal("no Ready event was published")
	}

	select {
	case state := <-h.ready:
		if state.EntityID != 7 {
			t.Errorf("ready state carries entity %d, want 7", state.EntityID)
		}
	default:
		t.Fatal("nothing was sent on the ready channel")
	}
}

func TestReadinessReplyIsReportedAsAPacketSent(t *testing.T) {
	t.Parallel()

	h := newHarness(t, "", 16, "position")

	sub, _ := h.client.events.subscribe(event.DomainRaw, 16)
	if err := h.run(t.Context(), &countingReadiness{}); err != nil {
		t.Fatalf("runLoop: %v", err)
	}
	_ = sub.Close()

	var sent []string
	for e := range sub.C() {
		if packet, ok := e.(event.PacketSent); ok {
			sent = append(sent, packet.Packet)
		}
	}

	if len(sent) != 1 || sent[0] != "teleport_confirm" {
		t.Errorf("raw subscribers saw sent packets %v, want one teleport_confirm", sent)
	}
}

func TestAFailedReplyWriteStopsTheLoop(t *testing.T) {
	t.Parallel()

	h := newHarness(t, "", 16, "position")
	sentinel := errors.New("connection gone")

	err := h.client.runLoop(
		t.Context(), h.receiver, failingSender{err: sentinel}, nil,
		h.batcher, h.collector, h.outbox, &countingReadiness{}, h.ready,
	)
	if !errors.Is(err, sentinel) {
		t.Fatalf("got %v, want the write error", err)
	}
}

func TestReadinessErrorStopsTheLoop(t *testing.T) {
	t.Parallel()

	h := newHarness(t, "", 16, "position")
	sentinel := errors.New("relative spawn")

	err := h.run(t.Context(), failingReadiness{err: sentinel})
	if !errors.Is(err, sentinel) {
		t.Fatalf("got %v, want the readiness rule's error", err)
	}
}

func TestDispatcherSeesEveryPacketInWireOrder(t *testing.T) {
	t.Parallel()

	h := newHarness(t, "", 16, "keep_alive", "chat", "keep_alive")
	handler := &recordingHandler{}

	err := h.client.runLoop(
		t.Context(), h.receiver, h.sender,
		newTableDispatcher(map[string]version.Handler{"keep_alive": handler}),
		h.batcher, h.collector, h.outbox, &countingReadiness{}, h.ready,
	)
	if err != nil {
		t.Fatalf("runLoop: %v", err)
	}

	// An unregistered packet is not an error, and a registered one is
	// dispatched every time it arrives.
	if len(handler.names) != 2 {
		t.Fatalf("handler saw %v, want two keep_alive packets", handler.names)
	}
}

func TestAFailingHandlerStopsTheLoop(t *testing.T) {
	t.Parallel()

	h := newHarness(t, "", 16, "keep_alive")
	sentinel := errors.New("handler broke")

	err := h.client.runLoop(
		t.Context(), h.receiver, h.sender,
		newTableDispatcher(map[string]version.Handler{"keep_alive": failingHandler{err: sentinel}}),
		h.batcher, h.collector, h.outbox, &countingReadiness{}, h.ready,
	)
	if !errors.Is(err, sentinel) {
		t.Fatalf("got %v, want the handler's error", err)
	}
}

func TestReadyIsNotSignalledTwice(t *testing.T) {
	t.Parallel()

	h := newHarness(t, "", 16, "position", "position")
	// The channel holds one value and nothing drains it. A second send that
	// blocked would deadlock the loop; a second send that succeeded would
	// mean Connect could return twice.
	rule := &countingReadiness{}

	if err := h.run(t.Context(), rule); err != nil {
		t.Fatalf("runLoop: %v", err)
	}
	if rule.calls != 2 {
		t.Errorf("readiness saw %d batches, want 2", rule.calls)
	}
	if len(h.ready) != 1 {
		t.Errorf("ready channel holds %d states, want 1", len(h.ready))
	}
}

// queueingHandler stands in for an adapter handler that answers.
type queueingHandler struct {
	outbox *version.Outbox
	answer string
}

func (h queueingHandler) Handle(context.Context, protocol.Packet) error {
	h.outbox.Add(protocol.Packet{Name: h.answer, State: "play"})

	return nil
}

func TestHandlerAnswersAreWrittenAndReported(t *testing.T) {
	t.Parallel()

	h := newHarness(t, "", 16, "keep_alive")
	sub, _ := h.client.events.subscribe(event.DomainRaw, 16)

	err := h.client.runLoop(
		t.Context(), h.receiver, h.sender,
		newTableDispatcher(map[string]version.Handler{
			"keep_alive": queueingHandler{outbox: h.outbox, answer: "keep_alive"},
		}),
		h.batcher, h.collector, h.outbox, &countingReadiness{}, h.ready,
	)
	if err != nil {
		t.Fatalf("runLoop: %v", err)
	}
	_ = sub.Close()

	if len(h.sender.sent) != 1 || h.sender.sent[0].Name != "keep_alive" {
		t.Fatalf("sent %v, want the handler's answer", h.sender.sent)
	}

	var reported int
	for e := range sub.C() {
		if packet, ok := e.(event.PacketSent); ok && packet.Packet == "keep_alive" {
			reported++
		}
	}
	if reported != 1 {
		t.Errorf("the answer was reported %d times as a sent packet, want 1", reported)
	}
}

func TestHandlerAnswersGoOutBeforeTheReadinessReply(t *testing.T) {
	t.Parallel()

	// One batch, one handler answer, and a readiness reply. The answer to
	// what the server asked comes first; the readiness reply is this client
	// moving on.
	h := newHarness(t, "", 16, "position")

	err := h.client.runLoop(
		t.Context(), h.receiver, h.sender,
		newTableDispatcher(map[string]version.Handler{
			"position": queueingHandler{outbox: h.outbox, answer: "answer"},
		}),
		h.batcher, h.collector, h.outbox, &countingReadiness{}, h.ready,
	)
	if err != nil {
		t.Fatalf("runLoop: %v", err)
	}

	if len(h.sender.sent) != 2 {
		t.Fatalf("sent %v, want the answer and the readiness reply", h.sender.sent)
	}
	if h.sender.sent[0].Name != "answer" || h.sender.sent[1].Name != "teleport_confirm" {
		t.Errorf("sent %v in the wrong order", h.sender.sent)
	}
}

func TestAnAnswerFromOneBatchIsNotResentByTheNext(t *testing.T) {
	t.Parallel()

	// The outbox is scoped to one batch. A stale answer resent on the next
	// batch would be a duplicate keepalive, or worse, a duplicate action.
	h := newHarness(t, "", 16, "keep_alive", "chat", "chat")

	err := h.client.runLoop(
		t.Context(), h.receiver, h.sender,
		newTableDispatcher(map[string]version.Handler{
			"keep_alive": queueingHandler{outbox: h.outbox, answer: "keep_alive"},
		}),
		h.batcher, h.collector, h.outbox, &countingReadiness{}, h.ready,
	)
	if err != nil {
		t.Fatalf("runLoop: %v", err)
	}

	if len(h.sender.sent) != 1 {
		t.Errorf("sent %v, want exactly one answer across three batches", h.sender.sent)
	}
}

func TestAFailedAnswerWriteStopsTheLoop(t *testing.T) {
	t.Parallel()

	h := newHarness(t, "", 16, "keep_alive")
	sentinel := errors.New("connection gone")

	err := h.client.runLoop(
		t.Context(), h.receiver, failingSender{err: sentinel},
		newTableDispatcher(map[string]version.Handler{
			"keep_alive": queueingHandler{outbox: h.outbox, answer: "keep_alive"},
		}),
		h.batcher, h.collector, h.outbox, &countingReadiness{}, h.ready,
	)
	if !errors.Is(err, sentinel) {
		t.Fatalf("got %v, want the write error", err)
	}
}

func TestEachBatchAdvancesTheWorldRevisionOnce(t *testing.T) {
	t.Parallel()

	h := newHarness(t, "bundle", 16, "bundle", "a", "b", "bundle", "c")
	w := world.New()
	h.client.world = w

	if err := h.run(t.Context(), &countingReadiness{}); err != nil {
		t.Fatalf("runLoop: %v", err)
	}

	// One bundle plus one loose packet is two batches, not three packets.
	if got := w.Snapshot().Revision; got != 2 {
		t.Errorf("world revision is %d, want 2", got)
	}
}

func TestEventsCarryTheRevisionThatProducedThem(t *testing.T) {
	t.Parallel()

	// A subscriber must be able to correlate an event with a snapshot. An
	// event published before the bump would name a revision that does not
	// yet exist.
	h := newHarness(t, "", 16, "a", "b")
	h.client.world = world.New()
	sub, _ := h.client.events.subscribe(event.DomainRaw, 16)

	if err := h.run(t.Context(), &countingReadiness{}); err != nil {
		t.Fatalf("runLoop: %v", err)
	}
	_ = sub.Close()

	var revisions []uint64
	for e := range sub.C() {
		if _, ok := e.(event.PacketReceived); ok {
			revisions = append(revisions, e.Revision())
		}
	}

	want := []uint64{1, 2}
	if len(revisions) != len(want) {
		t.Fatalf("published revisions %v, want %v", revisions, want)
	}
	for i := range want {
		if revisions[i] != want[i] {
			t.Fatalf("published revisions %v, want %v", revisions, want)
		}
	}
}

func TestAConfigurationBatchReachesTheWorld(t *testing.T) {
	t.Parallel()

	// Registry data arrives once, in configuration. A loop that started
	// applying at play would drop it, and the failure would surface as an
	// empty registry domain rather than as a wiring fault.
	h := newHarness(t, "", 16)
	h.receiver = &sliceReceiver{packets: []protocol.Packet{
		{Name: "registry_data", State: "configuration", Direction: protocol.DirectionClientbound},
	}}

	w := world.New()
	var seen []string
	_ = w.Register(world.Func(func(ctx *world.Context, _ version.Batch, _ *event.Collector) error {
		seen = append(seen, ctx.State)

		return nil
	}))
	h.client.world = w

	if err := h.run(t.Context(), &countingReadiness{}); err != nil {
		t.Fatalf("runLoop: %v", err)
	}

	if len(seen) != 1 || seen[0] != "configuration" {
		t.Errorf("the world saw batches in states %v, want one configuration batch", seen)
	}
}

func TestAWorldErrorStopsTheLoop(t *testing.T) {
	t.Parallel()

	// A poisoned world can no longer describe the server's state, so the
	// session ends rather than continuing to publish from it.
	h := newHarness(t, "", 16, "a", "b")
	w := world.New()
	_ = w.Register(world.Func(func(*world.Context, version.Batch, *event.Collector) error {
		return errWorldTest
	}))
	h.client.world = w

	err := h.run(t.Context(), &countingReadiness{})
	if !errors.Is(err, world.ErrWorldPoisoned) {
		t.Fatalf("got %v, want ErrWorldPoisoned", err)
	}
}

var errWorldTest = errors.New("reducer invariant broke")

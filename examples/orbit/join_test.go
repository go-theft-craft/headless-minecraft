package main

import (
	"log/slog"
	"testing"
	"time"

	"github.com/go-theft-craft/headless-minecraft/world"
)

// silent is the real M7 adapter over a world that has applied nothing, which is
// a server that reached play and sent no spawn position. It is the honest stand
// in for a world that cannot answer, now that the adapter is real: a hand
// written stub could not prove that an unsent spawn reads as unknown rather
// than as the origin.
func silent() Observed {
	return NewObserved(world.New().Snapshot(), PendingSolidity{})
}

func TestTheBotGivesUpWhenTheWorldNeverSuppliesSpawn(t *testing.T) {
	t.Parallel()

	// Standing in silence forever looks like a working bot, so the wait is
	// bounded and says what did not arrive.
	c := newClock()
	bot := NewBot(DefaultBounds())
	w := silent()

	if action := bot.Advance(Tick{Now: c.now, Ready: true}, w); action.Kind != Stand {
		t.Fatalf("produced %v on the first ready tick, want Stand", action.Kind)
	}

	action := bot.Advance(Tick{
		Now:   c.advance(DefaultBounds().JoinTimeout + time.Second),
		Ready: true,
	}, w)

	if action.Kind != Exit {
		t.Fatalf("produced %v past the join timeout, want Exit", action.Kind)
	}
	if action.Code != 3 {
		t.Errorf("exited %d, want the reserved 3 for an unimplemented port", action.Code)
	}
	if action.Reason == "" {
		t.Error("gave up without saying why")
	}
}

func TestTheJoinClockStartsAtPlayNotAtConstruction(t *testing.T) {
	t.Parallel()

	// Time spent connecting is not the world failing to answer. A bot that
	// waited two minutes for a slow login must still get its full window.
	c := newClock()
	bot := NewBot(DefaultBounds())

	bot.Advance(Tick{Now: c.advance(5 * time.Minute), Ready: false}, silent())

	if action := bot.Advance(Tick{Now: c.advance(time.Second), Ready: true}, silent()); action.Kind != Stand {
		t.Errorf("produced %v on reaching play late, want Stand", action.Kind)
	}
}

func TestAWorldThatAnswersInTimeStillJoins(t *testing.T) {
	t.Parallel()

	c := newClock()
	bot := NewBot(DefaultBounds())
	w := newScripted()

	bot.Advance(Tick{Now: c.now, Ready: true, Self: Self{Position: w.spawn}}, w)

	if bot.State() != Returning {
		t.Errorf("the bot is %v with a world that answered, want returning", bot.State())
	}
}

func TestNarrateLogsChangesAndNotEveryTick(t *testing.T) {
	t.Parallel()

	var (
		lines  int
		last   narration
		logger = slog.New(slog.NewTextHandler(countingWriter{&lines}, nil))
		core   = NewBot(DefaultBounds())
	)

	narrate(logger, core, Action{Kind: Stand, Reason: "waiting for play"}, &last)
	for range 20 {
		narrate(logger, core, Action{Kind: Stand, Reason: "waiting for play"}, &last)
	}
	narrate(logger, core, Action{Kind: Stand, Reason: "waiting for world spawn"}, &last)

	if lines != 2 {
		t.Errorf("logged %d lines for 22 ticks with 2 distinct reasons, want 2", lines)
	}
}

type countingWriter struct{ lines *int }

func (c countingWriter) Write(p []byte) (int, error) {
	*c.lines++

	return len(p), nil
}

func TestDyingBeforeTheCircleExistsResumesTheJoin(t *testing.T) {
	t.Parallel()

	// A live run crashed here. Death preempts every state including Joining, so
	// a client that connects to a server where it is already dead reaches Dead
	// without ever building a circle; respawning into Returning then walked
	// toward a circle of zero waypoints and divided by it.
	c := newClock()
	bot := NewBot(DefaultBounds())

	if action := bot.Advance(Tick{Now: c.now, Ready: true, Died: true}, silent()); action.Kind != SendRespawn {
		t.Fatalf("produced %v on dying, want SendRespawn", action.Kind)
	}

	bot.Advance(Tick{Now: c.advance(time.Second), Ready: true, Respawned: true}, silent())

	if bot.State() != Joining {
		t.Errorf("respawned into %v with no circle, want joining", bot.State())
	}

	// And the tick after must not panic on the circle it still does not have.
	w := newScripted()
	if action := bot.Advance(Tick{Now: c.advance(time.Second), Ready: true}, w); action.Kind == Exit {
		t.Errorf("gave up after resuming the join: %s", action.Reason)
	}
	if bot.State() == Joining {
		t.Error("stayed joining with a world that answered")
	}
}

package main

import (
	"log/slog"
	"testing"
	"time"
)

func TestTheBotGivesUpWhenTheWorldNeverSuppliesSpawn(t *testing.T) {
	t.Parallel()

	// Exactly what a live run does today: connect, reach play, and wait on a
	// world port that cannot answer. Standing in silence forever looks like a
	// working bot, so the wait is bounded and says which milestone it is on.
	c := newClock()
	bot := NewBot(DefaultBounds())
	world := Pending{}

	if action := bot.Advance(Tick{Now: c.now, Ready: true}, world); action.Kind != Stand {
		t.Fatalf("produced %v on the first ready tick, want Stand", action.Kind)
	}

	action := bot.Advance(Tick{
		Now:   c.advance(DefaultBounds().JoinTimeout + time.Second),
		Ready: true,
	}, world)

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

	bot.Advance(Tick{Now: c.advance(5 * time.Minute), Ready: false}, Pending{})

	if action := bot.Advance(Tick{Now: c.advance(time.Second), Ready: true}, Pending{}); action.Kind != Stand {
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

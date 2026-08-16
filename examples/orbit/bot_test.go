package main

import (
	"testing"
	"time"
)

// A tick source that advances a fake clock, so ten minutes of standing still
// costs a microsecond and the tests never sleep.
type clock struct{ now time.Time }

func newClock() *clock { return &clock{now: time.Unix(1_700_000_000, 0)} }

func (c *clock) advance(d time.Duration) time.Time {
	c.now = c.now.Add(d)

	return c.now
}

// join drives a fresh bot to Orbiting and returns it with its world.
func join(t *testing.T, w *scripted, start Vec3) (*Bot, *clock) {
	t.Helper()

	c := newClock()
	bot := NewBot(DefaultBounds())

	// Joining -> Returning
	bot.Advance(Tick{Now: c.now, Ready: true, Self: Self{Position: start}}, w)
	if bot.State() != Returning {
		t.Fatalf("after joining the bot is %v, want returning", bot.State())
	}

	return bot, c
}

// run steps the bot until it reaches a state or the budget runs out. It returns
// the last action.
func advanceTo(t *testing.T, bot *Bot, w World, c *clock, self func() Self, want State, steps int) Action {
	t.Helper()

	var last Action
	for range steps {
		last = bot.Advance(Tick{Now: c.advance(50 * time.Millisecond), Ready: true, Self: self()}, w)
		if bot.State() == want {
			return last
		}
	}

	t.Fatalf("bot is %v after %d steps, want %v", bot.State(), steps, want)

	return last
}

func TestTheBotWalksTheCircleAndAdvancesWaypoints(t *testing.T) {
	t.Parallel()

	w := newScripted()
	circle := NewCircle(w.spawn, 25, 32)
	position := circle.At(0, 0)

	bot, c := join(t, w, position)
	advanceTo(t, bot, w, c, func() Self { return Self{Position: position} }, Orbiting, 4)

	// Teleport the bot onto each waypoint in turn; the core must advance.
	seen := map[int]bool{}
	for range 40 {
		action := bot.Advance(Tick{
			Now:   c.advance(50 * time.Millisecond),
			Ready: true,
			Self:  Self{Position: position},
		}, w)

		if action.Kind != StepTo {
			t.Fatalf("orbiting produced %v, want StepTo", action.Kind)
		}
		if !action.Jump {
			t.Error("the bot is not jumping; jumping in a circle is the point")
		}
		seen[bot.Waypoint()%circle.Waypoints] = true
		position = action.Target
	}

	if len(seen) < 30 {
		t.Errorf("visited %d distinct waypoints in 40 ticks, want a full revolution", len(seen))
	}
}

func TestAWallIsBypassedInsideTheBand(t *testing.T) {
	t.Parallel()

	w := newScripted()
	circle := NewCircle(w.spawn, 25, 32)

	// Wall the circle at waypoint 1 and one block either side of it radially,
	// leaving the band open beyond.
	for _, offset := range []float64{0, -1, 1} {
		p := circle.At(1, offset).Floor()
		w.wall(p.X, p.Z, 3)
	}

	// Stand on waypoint 0 and walk into the wall at waypoint 1. Standing on the
	// walled waypoint instead would prove nothing: the bot would arrive, count
	// it reached, and advance past the wall without ever testing it.
	position := circle.At(0, 0)
	bot, c := join(t, w, position)
	advanceTo(t, bot, w, c, func() Self { return Self{Position: position} }, Orbiting, 4)

	action := bot.Advance(Tick{Now: c.advance(50 * time.Millisecond), Ready: true, Self: Self{Position: position}}, w)

	if action.Kind != StepTo {
		t.Fatalf("blocked orbit produced %v, want a step around it", action.Kind)
	}
	offset := bot.Offset()
	if offset == 0 {
		t.Error("the bot stepped into the wall")
	}
	if offset < -4 || offset > 4 {
		t.Errorf("bypass offset %.0f left the band", offset)
	}
}

func TestASealedBandWithNoProgressBecomesTrapped(t *testing.T) {
	t.Parallel()

	w := newScripted()
	circle := NewCircle(w.spawn, 25, 32)
	// Seal every waypoint the skip budget can reach, so skipping does not help.
	for i := range 8 {
		w.seal(circle, i, 4)
	}

	position := circle.At(0, 0)
	bot, c := join(t, w, position)
	advanceTo(t, bot, w, c, func() Self { return Self{Position: position} }, Orbiting, 4)

	// Before the no-progress window elapses the bot waits rather than declaring
	// itself trapped: a mob in the way exhausts the search and then walks off.
	for range 5 {
		bot.Advance(Tick{Now: c.advance(time.Second), Ready: true, Self: Self{Position: position}}, w)
	}
	if bot.State() == Trapped {
		t.Fatal("declared trapped inside the no-progress window")
	}

	advanceTo(t, bot, w, c, func() Self { return Self{Position: position} }, Trapped, 400)
}

func TestATrappedBotStandsAndSendsNothing(t *testing.T) {
	t.Parallel()

	bot, c, w, position := trapped(t)

	for range 20 {
		action := bot.Advance(Tick{
			Now:      c.advance(time.Second),
			Ready:    true,
			Self:     Self{Position: position},
			Revision: 7,
		}, w)

		if action.Kind != Stand {
			t.Fatalf("a trapped bot produced %v, want Stand", action.Kind)
		}
	}
}

func TestATrappedBotResumesWhenTheWallChanges(t *testing.T) {
	t.Parallel()

	bot, c, w, position := trapped(t)
	circle := NewCircle(w.spawn, 25, 32)

	// Nothing changes while the revision does not.
	if action := bot.Advance(Tick{Now: c.advance(time.Second), Ready: true, Self: Self{Position: position}, Revision: 1}, w); action.Kind != Stand {
		t.Fatalf("produced %v with an unchanged revision, want Stand", action.Kind)
	}

	// Break one column out of the wall and bump the revision, which is how M7
	// reports a block change.
	p := circle.At(bot.Waypoint(), 2).Floor()
	for y := 64; y < 68; y++ {
		delete(w.solid, BlockPos{X: p.X, Y: y, Z: p.Z})
	}

	action := bot.Advance(Tick{
		Now:      c.advance(time.Second),
		Ready:    true,
		Self:     Self{Position: position},
		Revision: 2,
	}, w)

	if bot.State() != Orbiting {
		t.Fatalf("the bot is %v after the wall opened, want orbiting", bot.State())
	}
	if action.Kind != StepTo {
		t.Errorf("produced %v, want a step through the opening", action.Kind)
	}
}

func TestATrappedBotGivesUpAfterItsBudget(t *testing.T) {
	t.Parallel()

	bot, c, w, position := trapped(t)

	action := bot.Advance(Tick{
		Now:      c.advance(DefaultBounds().TrappedBudget + time.Second),
		Ready:    true,
		Self:     Self{Position: position},
		Revision: 1,
	}, w)

	if action.Kind != Exit {
		t.Fatalf("produced %v after the trapped budget, want Exit", action.Kind)
	}
	if action.Code == 0 {
		t.Error("exited zero; a bot sealed in for ten minutes did not do its job")
	}
	if bot.State() != Done {
		t.Errorf("the bot is %v, want done", bot.State())
	}
}

// trapped returns a bot standing in a sealed band.
func trapped(t *testing.T) (*Bot, *clock, *scripted, Vec3) {
	t.Helper()

	w := newScripted()
	circle := NewCircle(w.spawn, 25, 32)
	for i := range 8 {
		w.seal(circle, i, 4)
	}

	position := circle.At(0, 0)
	bot, c := join(t, w, position)
	advanceTo(t, bot, w, c, func() Self { return Self{Position: position} }, Orbiting, 4)
	advanceTo(t, bot, w, c, func() Self { return Self{Position: position} }, Trapped, 800)

	return bot, c, w, position
}

func TestBeingHitStartsAFightWithTheSource(t *testing.T) {
	t.Parallel()

	w := newScripted()
	circle := NewCircle(w.spawn, 25, 32)
	position := circle.At(0, 0)

	// The attacker is in reach.
	w.entities[42] = Entity{ID: 42, Position: position, Health: 20, Alive: true}

	bot, c := join(t, w, position)
	advanceTo(t, bot, w, c, func() Self { return Self{Position: position} }, Orbiting, 4)

	action := bot.Advance(Tick{
		Now:      c.advance(50 * time.Millisecond),
		Ready:    true,
		Self:     Self{Position: position},
		Attacker: 42,
	}, w)

	if bot.State() != Engaging {
		t.Fatalf("the bot is %v after being hit, want engaging", bot.State())
	}
	if action.Kind != Strike || action.Entity != 42 {
		t.Errorf("produced %+v, want a strike at 42", action)
	}
}

func TestAFightEndsWhenTheTargetLeavesTheNeighbourhood(t *testing.T) {
	t.Parallel()

	w := newScripted()
	circle := NewCircle(w.spawn, 25, 32)
	position := circle.At(0, 0)
	w.entities[42] = Entity{ID: 42, Position: position, Health: 20, Alive: true}

	bot, c := join(t, w, position)
	advanceTo(t, bot, w, c, func() Self { return Self{Position: position} }, Orbiting, 4)
	bot.Advance(Tick{Now: c.advance(50 * time.Millisecond), Ready: true, Self: Self{Position: position}, Attacker: 42}, w)

	// Lead the bot away: the target runs well past the chase margin.
	w.entities[42] = Entity{ID: 42, Position: Vec3{X: 500, Y: 64, Z: 500}, Health: 20, Alive: true}

	bot.Advance(Tick{Now: c.advance(50 * time.Millisecond), Ready: true, Self: Self{Position: position}}, w)

	if bot.State() != Returning {
		t.Errorf("the bot is %v after the target fled, want returning", bot.State())
	}
}

func TestAFightEndsWhenTheTargetDies(t *testing.T) {
	t.Parallel()

	w := newScripted()
	circle := NewCircle(w.spawn, 25, 32)
	position := circle.At(0, 0)
	w.entities[42] = Entity{ID: 42, Position: position, Health: 20, Alive: true}

	bot, c := join(t, w, position)
	advanceTo(t, bot, w, c, func() Self { return Self{Position: position} }, Orbiting, 4)
	bot.Advance(Tick{Now: c.advance(50 * time.Millisecond), Ready: true, Self: Self{Position: position}, Attacker: 42}, w)

	w.entities[42] = Entity{ID: 42, Position: position, Alive: false}
	bot.Advance(Tick{Now: c.advance(50 * time.Millisecond), Ready: true, Self: Self{Position: position}}, w)

	if bot.State() != Returning {
		t.Errorf("the bot is %v after killing the target, want returning", bot.State())
	}
}

func TestAFightTimesOutOnAnUnkillableTarget(t *testing.T) {
	t.Parallel()

	w := newScripted()
	circle := NewCircle(w.spawn, 25, 32)
	position := circle.At(0, 0)
	w.entities[42] = Entity{ID: 42, Position: position, Health: 20, Alive: true}

	bot, c := join(t, w, position)
	advanceTo(t, bot, w, c, func() Self { return Self{Position: position} }, Orbiting, 4)
	bot.Advance(Tick{Now: c.advance(50 * time.Millisecond), Ready: true, Self: Self{Position: position}, Attacker: 42}, w)

	bot.Advance(Tick{
		Now:   c.advance(DefaultBounds().Engagement + time.Second),
		Ready: true,
		Self:  Self{Position: position},
	}, w)

	if bot.State() != Returning {
		t.Errorf("the bot is %v after the engagement timeout, want returning", bot.State())
	}
}

func TestDeathSendsOneRespawnAndReturnsToTheCircle(t *testing.T) {
	t.Parallel()

	w := newScripted()
	circle := NewCircle(w.spawn, 25, 32)
	position := circle.At(0, 0)

	bot, c := join(t, w, position)
	advanceTo(t, bot, w, c, func() Self { return Self{Position: position} }, Orbiting, 4)

	action := bot.Advance(Tick{Now: c.advance(50 * time.Millisecond), Ready: true, Self: Self{Position: position}, Died: true}, w)
	if action.Kind != SendRespawn {
		t.Fatalf("death produced %v, want SendRespawn", action.Kind)
	}

	// One respawn, not one per tick. The library does not retry ambiguous work
	// and neither does this.
	for range 5 {
		if got := bot.Advance(Tick{Now: c.advance(50 * time.Millisecond), Ready: true, Self: Self{Position: position}}, w); got.Kind != Stand {
			t.Fatalf("produced %v while awaiting respawn, want Stand", got.Kind)
		}
	}

	// Respawn far from the circle, which is what a bed does.
	far := Vec3{X: 200, Y: 64, Z: 200}
	bot.Advance(Tick{Now: c.advance(50 * time.Millisecond), Ready: true, Self: Self{Position: far}, Respawned: true}, w)
	if bot.State() != Returning {
		t.Fatalf("the bot is %v after respawning, want returning", bot.State())
	}

	action = bot.Advance(Tick{Now: c.advance(50 * time.Millisecond), Ready: true, Self: Self{Position: far}}, w)
	if action.Kind != StepTo {
		t.Fatalf("produced %v while returning, want a step", action.Kind)
	}
	// It walks back toward the circle, not to wherever it died.
	if got := action.Target.HorizontalDistance(circle.Centre); got < 24 || got > 26 {
		t.Errorf("returning toward a point %.1f from spawn, want the circle at 25", got)
	}
}

func TestRepeatedCorrectionsStopTheBot(t *testing.T) {
	t.Parallel()

	w := newScripted()
	circle := NewCircle(w.spawn, 25, 32)
	position := circle.At(0, 0)

	bot, c := join(t, w, position)
	advanceTo(t, bot, w, c, func() Self { return Self{Position: position} }, Orbiting, 4)

	bounds := DefaultBounds()
	for i := range bounds.BreakerBudget {
		action := bot.Advance(Tick{Now: c.advance(50 * time.Millisecond), Ready: true, Self: Self{Position: position}, Corrected: true}, w)
		if action.Kind == Exit {
			t.Fatalf("gave up on correction %d of %d", i+1, bounds.BreakerBudget)
		}
	}

	action := bot.Advance(Tick{Now: c.advance(50 * time.Millisecond), Ready: true, Self: Self{Position: position}, Corrected: true}, w)
	if action.Kind != Exit {
		t.Fatalf("produced %v past the breaker budget, want Exit", action.Kind)
	}
	if action.Code == 0 {
		t.Error("exited zero after repeated corrections")
	}
}

func TestTheBotWaitsForSpawnBeforeBuildingACircle(t *testing.T) {
	t.Parallel()

	w := newScripted()
	w.hasSpawn = false
	c := newClock()
	bot := NewBot(DefaultBounds())

	if action := bot.Advance(Tick{Now: c.now, Ready: false}, w); action.Kind != Stand {
		t.Errorf("produced %v before play, want Stand", action.Kind)
	}
	if action := bot.Advance(Tick{Now: c.now, Ready: true}, w); action.Kind != Stand {
		t.Errorf("produced %v without a spawn position, want Stand", action.Kind)
	}
	if bot.State() != Joining {
		t.Errorf("the bot is %v without a spawn position, want joining", bot.State())
	}
}

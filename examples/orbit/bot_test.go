package main

import (
	"math"
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
func join(t *testing.T, w World, start Vec3) (*Bot, *clock) {
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

// revolutionTicks is how many ticks a full revolution takes when the test
// teleports the bot to whatever the core aimed at.
//
// It used to be forty, one per waypoint, because the core aimed straight at the
// next waypoint and the test jumped the whole five blocks. The core now walks a
// planned route and aims at the next cell on it, so a tick covers one block and
// a circle of radius 25 is about a hundred and sixty of them.
const revolutionTicks = 400

func TestTheBotWalksTheCircleAndAdvancesWaypoints(t *testing.T) {
	t.Parallel()

	w := newScripted()
	circle := NewCircle(w.spawn, 25, 32)
	position := circle.At(0, 0)

	bot, c := join(t, w, position)
	advanceTo(t, bot, w, c, func() Self { return Self{Position: position} }, Orbiting, 4)

	// Teleport the bot onto each waypoint in turn; the core must advance.
	seen := map[int]bool{}
	for range revolutionTicks {
		action := bot.Advance(Tick{
			Now:   c.advance(50 * time.Millisecond),
			Ready: true,
			Self:  Self{Position: position},
		}, w)

		if action.Kind != StepTo {
			t.Fatalf("orbiting produced %v, want StepTo", action.Kind)
		}
		// Not jumping, and saying so. There is no body to jump with, and the
		// flag now reaches the wire as part of the locomotion the bot
		// declares, so setting it would be a claim about something that does
		// not happen.
		if action.Jump {
			t.Error("the bot claimed a jump it has no body to perform")
		}
		seen[bot.Waypoint()%circle.Waypoints] = true
		position = action.Target
	}

	if len(seen) < 30 {
		t.Errorf("visited %d distinct waypoints in %d ticks, want a full revolution", len(seen), revolutionTicks)
	}
}

// TestAWaypointNothingCanRouteToIsSkipped pins what the example still decides.
//
// Going around an obstacle is the planner's job now and is tested where the
// planner lives. What is left here is the answer to a planner that comes back
// with nothing: skip the waypoint and carry on round, rather than walk at it
// anyway. Walking at it anyway is what the old radial-band search did when its
// band was exhausted, and it cost two live runs.
func TestAWaypointNothingCanRouteToIsSkipped(t *testing.T) {
	t.Parallel()

	w := newScripted()
	circle := NewCircle(w.spawn, 25, 32)

	// Wall the circle at waypoint 1 and one block either side of it radially.
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

	if action.Kind != Stand {
		t.Fatalf("produced %v at a waypoint nothing routes to, want it to skip", action.Kind)
	}
	if bot.Waypoint() == 1 {
		t.Error("the bot is still heading at the wall")
	}
	if len(bot.Route().Steps) != 0 {
		t.Errorf("kept a route of %d steps to somewhere unreachable", len(bot.Route().Steps))
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

	// Take the whole wall down and bump the revision, which is how M7 reports
	// a block change.
	//
	// All of it, not one column of it. Which single column would open a way
	// through depends on where the planner would route and how wide the body
	// is, and pinning that here would be testing the planner. What this test
	// is for is narrower: the bot re-asks when the world changes, and walks
	// when the answer comes back different.
	for waypoint := range 8 {
		for offset := -4; offset <= 4; offset++ {
			p := circle.At(waypoint, float64(offset)).Floor()
			for y := 64; y < 68; y++ {
				delete(w.solid, BlockPos{X: p.X, Y: y, Z: p.Z})
			}
		}
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

func TestBeingHitSendsTheBotRunningTheOtherWay(t *testing.T) {
	t.Parallel()

	w := newScripted()
	circle := NewCircle(w.spawn, 25, 32)
	position := circle.At(0, 0)

	// Two blocks away on +X, so away is -X and the direction is unambiguous.
	attacker := position.Add(Vec3{X: 2})
	w.entities[42] = Entity{ID: 42, Position: attacker, Health: 20, Alive: true}

	bot, c := join(t, w, position)
	advanceTo(t, bot, w, c, func() Self { return Self{Position: position} }, Orbiting, 4)

	action := bot.Advance(Tick{
		Now:      c.advance(50 * time.Millisecond),
		Ready:    true,
		Self:     Self{Position: position},
		Attacker: 42,
	}, w)

	if bot.State() != Fleeing {
		t.Fatalf("the bot is %v after being hit, want fleeing", bot.State())
	}
	if action.Kind != StepTo {
		t.Fatalf("produced %+v, want a step", action)
	}
	// Away, and routed: the bot aims at the first leg of a way out rather than
	// at the destination, so the destination is the end of the route and the
	// step only has to be heading there.
	route := bot.Route()
	if len(route.Steps) == 0 {
		t.Fatal("fleeing without a route")
	}
	want := position.X - DefaultBounds().SafeDistance
	if last := route.Steps[len(route.Steps)-1]; math.Abs(last.X-want) > 1e-9 ||
		math.Abs(last.Z-position.Z) > 1e-9 {
		t.Errorf("the route ends at %+v, want (%v, _, %v)", last, want, position.Z)
	}
	if action.Target.X >= position.X {
		t.Errorf("stepped to %+v, which is not away from the attacker", action.Target)
	}
}

// TestTheBotRunsEvenWhenTheThreatIsStandingOnIt pins the degenerate direction.
//
// A mob that has walked into the bot shares its horizontal position, and the
// direction away from it is then undefined. Standing still is the one answer
// that is certainly wrong, because whatever is there is hitting the bot.
func TestTheBotRunsEvenWhenTheThreatIsStandingOnIt(t *testing.T) {
	t.Parallel()

	w := newScripted()
	circle := NewCircle(w.spawn, 25, 32)
	position := circle.At(0, 0)
	w.entities[42] = Entity{ID: 42, Position: position, Health: 20, Alive: true}

	bot, c := join(t, w, position)
	advanceTo(t, bot, w, c, func() Self { return Self{Position: position} }, Orbiting, 4)

	action := bot.Advance(Tick{
		Now:      c.advance(50 * time.Millisecond),
		Ready:    true,
		Self:     Self{Position: position},
		Attacker: 42,
	}, w)

	if action.Kind != StepTo {
		t.Fatalf("produced %+v, want a step", action)
	}
	if action.Target.HorizontalDistance(position) == 0 {
		t.Error("the bot stood still on top of the thing hitting it")
	}
}

func TestRunningEndsOnceTheBotIsClearOfTheThreat(t *testing.T) {
	t.Parallel()

	w := newScripted()
	circle := NewCircle(w.spawn, 25, 32)
	position := circle.At(0, 0)
	w.entities[42] = Entity{ID: 42, Position: position, Health: 20, Alive: true}

	bot, c := join(t, w, position)
	advanceTo(t, bot, w, c, func() Self { return Self{Position: position} }, Orbiting, 4)
	bot.Advance(Tick{Now: c.advance(50 * time.Millisecond), Ready: true, Self: Self{Position: position}, Attacker: 42}, w)

	// The threat gives up and leaves, putting it well past the safe distance.
	w.entities[42] = Entity{ID: 42, Position: Vec3{X: 500, Y: 64, Z: 500}, Health: 20, Alive: true}

	bot.Advance(Tick{Now: c.advance(50 * time.Millisecond), Ready: true, Self: Self{Position: position}}, w)

	if bot.State() != Returning {
		t.Errorf("the bot is %v once clear of the threat, want returning", bot.State())
	}
}

func TestRunningEndsWhenTheThreatDies(t *testing.T) {
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
		t.Errorf("the bot is %v after the threat died, want returning", bot.State())
	}
}

func TestRunningStopsOnceItHasGoneOnLongEnough(t *testing.T) {
	t.Parallel()

	w := newScripted()
	circle := NewCircle(w.spawn, 25, 32)
	position := circle.At(0, 0)
	w.entities[42] = Entity{ID: 42, Position: position, Health: 20, Alive: true}

	bot, c := join(t, w, position)
	advanceTo(t, bot, w, c, func() Self { return Self{Position: position} }, Orbiting, 4)
	bot.Advance(Tick{Now: c.advance(50 * time.Millisecond), Ready: true, Self: Self{Position: position}, Attacker: 42}, w)

	bot.Advance(Tick{
		Now:   c.advance(DefaultBounds().Escape + time.Second),
		Ready: true,
		Self:  Self{Position: position},
	}, w)

	if bot.State() != Returning {
		t.Errorf("the bot is %v after the escape timeout, want returning", bot.State())
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
	// It walks back toward the circle, not to wherever it died. The step is the
	// first cell of a route now rather than the destination itself, so the
	// circle is where the route ends and the step is only required to be
	// heading there.
	route := bot.Route()
	if len(route.Steps) == 0 {
		t.Fatal("returning without a route")
	}
	if got := route.Steps[len(route.Steps)-1].HorizontalDistance(circle.Centre); got < 24 || got > 26 {
		t.Errorf("the route ends %.1f from spawn, want the circle at 25", got)
	}
	if action.Target.HorizontalDistance(circle.Centre) >= far.HorizontalDistance(circle.Centre) {
		t.Error("the first step did not close the distance to the circle")
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

// TestRunningStopsOnceTheBotHasLeftItsCircleBehind pins the leash.
//
// The bot runs from a threat, not to the horizon. Without this bound a mob
// that keeps pace would walk the bot off the map, and the circle it exists to
// orbit would never be seen again.
func TestRunningStopsOnceTheBotHasLeftItsCircleBehind(t *testing.T) {
	t.Parallel()

	w := newScripted()
	circle := NewCircle(w.spawn, 25, 32)
	position := circle.At(0, 0)
	bounds := DefaultBounds()

	// Past the radius and the margin both, with the threat still on the bot's
	// heels so that the distance to it is not what ends the run.
	far := w.spawn.Add(Vec3{X: circle.Radius + bounds.FleeMargin + 1})
	w.entities[42] = Entity{ID: 42, Position: far.Add(Vec3{X: 2}), Health: 20, Alive: true}

	bot, c := join(t, w, position)
	advanceTo(t, bot, w, c, func() Self { return Self{Position: position} }, Orbiting, 4)

	bot.Advance(Tick{
		Now:      c.advance(50 * time.Millisecond),
		Ready:    true,
		Self:     Self{Position: far},
		Attacker: 42,
	}, w)

	if bot.State() != Returning {
		t.Errorf("the bot is %v after running past its margin, want returning", bot.State())
	}
}

// TestSomethingThatCannotFollowDoesNotEndTheOrbit pins the point of naming
// entities at all.
//
// Leaving the circle costs the bot its orbit and a walk back. That is worth it
// for something chasing it and wasted on a minecart, and until the entity had
// a name the bot could not tell the two apart -- it ran from whatever the
// damage pointed at.
func TestSomethingThatCannotFollowDoesNotEndTheOrbit(t *testing.T) {
	t.Parallel()

	w := newScripted()
	circle := NewCircle(w.spawn, 25, 32)
	position := circle.At(0, 0)
	w.entities[42] = Entity{
		ID: 42, Position: position, Health: 20, Alive: true,
		Kind: Kind{Name: "Minecart", Pursues: false}, Named: true,
	}

	bot, c := join(t, w, position)
	advanceTo(t, bot, w, c, func() Self { return Self{Position: position} }, Orbiting, 4)

	bot.Advance(Tick{
		Now:      c.advance(50 * time.Millisecond),
		Ready:    true,
		Self:     Self{Position: position},
		Attacker: 42,
	}, w)

	if bot.State() != Orbiting {
		t.Errorf("the bot is %v after a minecart hit it, want it still orbiting", bot.State())
	}
}

// TestAnUnnamedAttackerIsStillRunFrom pins that the cautious default survives
// into the core, not just the lookup.
func TestAnUnnamedAttackerIsStillRunFrom(t *testing.T) {
	t.Parallel()

	w := newScripted()
	circle := NewCircle(w.spawn, 25, 32)
	position := circle.At(0, 0)
	// Named false, and a Kind that would say "harmless" if anything read it.
	w.entities[42] = Entity{
		ID: 42, Position: position.Add(Vec3{X: 2}), Health: 20, Alive: true,
		Kind: Kind{Pursues: false}, Named: false,
	}

	bot, c := join(t, w, position)
	advanceTo(t, bot, w, c, func() Self { return Self{Position: position} }, Orbiting, 4)

	bot.Advance(Tick{
		Now:      c.advance(50 * time.Millisecond),
		Ready:    true,
		Self:     Self{Position: position},
		Attacker: 42,
	}, w)

	if bot.State() != Fleeing {
		t.Errorf("the bot is %v after something it cannot name hit it, want fleeing", bot.State())
	}
}

// TestAWayOutNeverRunsAtTheThreat pins the rule the game states outright.
//
// Vanilla's avoid-entity goal throws out any destination no further from the
// thing being avoided than the mob already is. Without it a wide fan of
// headings will cheerfully pick one that closes the distance, which is not an
// escape however confidently it is walked.
func TestAWayOutNeverRunsAtTheThreat(t *testing.T) {
	t.Parallel()

	w := newScripted()
	circle := NewCircle(w.spawn, 25, 32)
	position := circle.At(0, 0)
	threat := Entity{
		ID: 42, Position: position.Add(Vec3{X: 2}), Health: 20, Alive: true,
		Kind: Kind{Name: "Zombie", Pursues: true}, Named: true,
	}
	w.entities[42] = threat

	bot, c := join(t, w, position)
	advanceTo(t, bot, w, c, func() Self { return Self{Position: position} }, Orbiting, 4)
	bot.Advance(Tick{
		Now:      c.advance(50 * time.Millisecond),
		Ready:    true,
		Self:     Self{Position: position},
		Attacker: 42,
	}, w)

	if bot.State() != Fleeing {
		t.Fatalf("the bot is %v, want fleeing", bot.State())
	}

	route := bot.Route()
	if len(route.Steps) == 0 {
		t.Fatal("fleeing without a route")
	}

	was := position.HorizontalDistance(threat.Position)
	for i, step := range route.Steps {
		if got := step.HorizontalDistance(threat.Position); got < was-0.001 && i == len(route.Steps)-1 {
			t.Errorf("the way out ends %.2f from the threat, closer than the %.2f it started at", got, was)
		}
	}
}

// TestNowhereToRunKeepsTheBotOnItsCircle pins that a cornered bot walks.
//
// It used to stand still, which is the one answer worse than either
// alternative: it is being hit either way, and standing gives up the orbit as
// well. The game does the same thing -- a mob whose avoid goal cannot find a
// path does not start it, and carries on with what it was doing.
func TestNowhereToRunKeepsTheBotOnItsCircle(t *testing.T) {
	t.Parallel()

	w := newScripted()
	circle := NewCircle(w.spawn, 25, 32)
	position := circle.At(0, 0)
	w.entities[42] = Entity{
		ID: 42, Position: position.Add(Vec3{X: 1}), Health: 20, Alive: true,
		Kind: Kind{Name: "Zombie", Pursues: true}, Named: true,
	}
	// Nothing routes anywhere: every destination is unreachable.
	w.loaded = func(BlockPos) bool { return false }

	bot, c := join(t, w, position)
	bot.Advance(Tick{
		Now:      c.advance(50 * time.Millisecond),
		Ready:    true,
		Self:     Self{Position: position},
		Attacker: 42,
	}, w)

	if bot.State() == Fleeing {
		t.Error("the bot started a flight it has nowhere to run to")
	}
}

// TestLavaPouredInFrontOfACommittedRouteIsNoticed reproduces the report.
//
// A route is planned once and walked over many ticks. Somebody pours lava
// across it while the bot is part way along, and nothing about the route the
// bot is holding changes -- so it walks into ground that was clear when it set
// off. It has to look again when the world says it changed.
func TestLavaPouredInFrontOfACommittedRouteIsNoticed(t *testing.T) {
	t.Parallel()

	w := newScripted()
	circle := NewCircle(w.spawn, 25, 32)
	position := circle.At(0, 0)

	bot, c := join(t, w, position)
	advanceTo(t, bot, w, c, func() Self { return Self{Position: position} }, Orbiting, 4)

	// Walking, with a route in hand.
	action := bot.Advance(Tick{
		Now: c.advance(50 * time.Millisecond), Ready: true,
		Self: Self{Position: position}, Revision: 1,
	}, w)
	if action.Kind != StepTo {
		t.Fatalf("produced %v before the pour, want a step", action.Kind)
	}
	if len(bot.Route().Steps) == 0 {
		t.Fatal("walking without a route")
	}

	// The pour: wall the ground a stride ahead, and bump the revision, which is
	// how the world reports that a block changed.
	blocked := position.Toward(bot.Route().Steps[0], 1)
	p := blocked.Floor()
	w.wall(p.X, p.Z, 3)

	action = bot.Advance(Tick{
		Now: c.advance(50 * time.Millisecond), Ready: true,
		Self: Self{Position: position}, Revision: 2,
	}, w)

	if action.Kind == StepTo && action.Target == blocked {
		t.Error("walked into the block that appeared in front of it")
	}
	if len(bot.Route().Steps) != 0 {
		t.Error("kept a route across ground that changed under it")
	}
}

// TestAnUnchangedWorldIsNotReExamined pins the cost.
//
// The check runs on a revision that changed, not on a timer. Re-examining the
// ground every tick regardless would pay for the world holding still, which is
// what it does almost all of the time.
func TestAnUnchangedWorldIsNotReExamined(t *testing.T) {
	t.Parallel()

	w := &counting{scripted: newScripted()}
	circle := NewCircle(w.spawn, 25, 32)
	position := circle.At(0, 0)

	bot, c := join(t, w, position)
	advanceTo(t, bot, w, c, func() Self { return Self{Position: position} }, Orbiting, 4)

	w.walkable = 0
	for range 10 {
		bot.Advance(Tick{
			Now: c.advance(50 * time.Millisecond), Ready: true,
			Self: Self{Position: position}, Revision: 7,
		}, w)
	}

	if w.walkable > 1 {
		t.Errorf("asked %d times about a world that never changed, want at most 1", w.walkable)
	}
}

// counting is a scripted world that records how often it was asked.
type counting struct {
	*scripted
	walkable int
}

func (c *counting) Walkable(from, to Vec3) bool {
	c.walkable++

	return c.scripted.Walkable(from, to)
}

// TestStandingInLavaOutranksEverything pins the priority.
//
// A bot deciding which waypoint comes next while it burns is a bot that
// finishes deciding somewhere it cannot be revived from. Getting out is above
// the orbit, above the flight and above the walk home; only dying outranks it.
func TestStandingInLavaOutranksEverything(t *testing.T) {
	t.Parallel()

	w := newScripted()
	circle := NewCircle(w.spawn, 25, 32)
	position := circle.At(0, 0)

	bot, c := join(t, w, position)
	advanceTo(t, bot, w, c, func() Self { return Self{Position: position} }, Orbiting, 4)

	// The ground the bot is standing on turns to lava.
	w.harmful[position.Floor()] = true

	action := bot.Advance(Tick{
		Now: c.advance(50 * time.Millisecond), Ready: true,
		Self: Self{Position: position}, Revision: 2,
	}, w)

	if bot.State() != Escaping {
		t.Fatalf("the bot is %v while standing in lava, want escaping", bot.State())
	}
	if action.Kind != StepTo {
		t.Errorf("produced %v while standing in lava, want a step out", action.Kind)
	}
}

// TestEscapingEndsWhenTheGroundIsSafeAgain pins the way back.
func TestEscapingEndsWhenTheGroundIsSafeAgain(t *testing.T) {
	t.Parallel()

	w := newScripted()
	circle := NewCircle(w.spawn, 25, 32)
	position := circle.At(0, 0)

	bot, c := join(t, w, position)
	advanceTo(t, bot, w, c, func() Self { return Self{Position: position} }, Orbiting, 4)

	w.harmful[position.Floor()] = true
	bot.Advance(Tick{
		Now: c.advance(50 * time.Millisecond), Ready: true,
		Self: Self{Position: position}, Revision: 2,
	}, w)
	if bot.State() != Escaping {
		t.Fatalf("the bot is %v, want escaping", bot.State())
	}

	// One step later it is standing somewhere that does not hurt.
	safe := position.Add(Vec3{X: 3})
	bot.Advance(Tick{
		Now: c.advance(50 * time.Millisecond), Ready: true,
		Self: Self{Position: safe}, Revision: 3,
	}, w)

	if bot.State() == Escaping {
		t.Error("still escaping from ground that no longer hurts")
	}
}

// TestUnstreamedGroundIsNotMistakenForLava pins the distinction that broke the
// first attempt at this: a world that has sent nothing is not a world on fire.
func TestUnstreamedGroundIsNotMistakenForLava(t *testing.T) {
	t.Parallel()

	w := newScripted()
	w.loaded = func(BlockPos) bool { return false }
	position := Vec3{Y: 64}

	bot := NewBot(DefaultBounds())
	c := newClock()
	bot.Advance(Tick{Now: c.advance(time.Second), Ready: true, Self: Self{Position: position}}, w)

	if bot.State() == Escaping {
		t.Error("the bot decided it was burning on ground nobody has described")
	}
}

// TestBurningWalksToWaterWorthReaching pins the arithmetic in the direction
// that saves the bot.
//
// Fire does a point a second and lava lights a body for fifteen, so water four
// seconds away with most of the fire left is six or seven points saved for a
// short walk.
func TestBurningWalksToWaterWorthReaching(t *testing.T) {
	t.Parallel()

	w := newScripted()
	circle := NewCircle(w.spawn, 25, 32)
	position := circle.At(0, 0)
	// Three blocks away: under a second of walking, against fifteen of fire.
	w.water[position.Add(Vec3{X: 3}).Floor()] = true

	bot, c := join(t, w, position)
	advanceTo(t, bot, w, c, func() Self { return Self{Position: position} }, Orbiting, 4)

	bot.Advance(Tick{
		Now: c.advance(50 * time.Millisecond), Ready: true,
		Self: Self{Position: position, OnFire: true},
	}, w)

	if bot.State() != Dousing {
		t.Errorf("the bot is %v while burning next to water, want dousing", bot.State())
	}
}

// TestBurningIgnoresWaterItCannotReachInTime pins the other half.
//
// The fire burns whether the bot walks or not, so a trip is only worth making
// if it ends before the fire would have anyway. Water further off than the
// fire is long costs the orbit and saves nothing.
func TestBurningIgnoresWaterItCannotReachInTime(t *testing.T) {
	t.Parallel()

	w := newScripted()
	circle := NewCircle(w.spawn, 25, 32)
	position := circle.At(0, 0)
	w.water[position.Add(Vec3{X: 10}).Floor()] = true

	bot, c := join(t, w, position)
	advanceTo(t, bot, w, c, func() Self { return Self{Position: position} }, Orbiting, 4)

	// Lit fourteen seconds ago: one second of fire left, and the water is two
	// and a half seconds of walking away.
	bot.Advance(Tick{
		Now: c.advance(50 * time.Millisecond), Ready: true,
		Self: Self{Position: position, OnFire: true},
	}, w)
	bot.Advance(Tick{
		Now: c.advance(14 * time.Second), Ready: true,
		Self: Self{Position: position, OnFire: true},
	}, w)

	if bot.State() == Dousing {
		t.Error("walked for water it cannot reach before the fire ends")
	}
}

// TestBurningInLavaGetsOutBeforeLookingForWater pins the order.
//
// Walking to water while standing in lava is pointless: the lava relights the
// fire every tick, so the fire the walk is meant to end never ends.
func TestBurningInLavaGetsOutBeforeLookingForWater(t *testing.T) {
	t.Parallel()

	w := newScripted()
	circle := NewCircle(w.spawn, 25, 32)
	position := circle.At(0, 0)
	w.water[position.Add(Vec3{X: 3}).Floor()] = true

	bot, c := join(t, w, position)
	advanceTo(t, bot, w, c, func() Self { return Self{Position: position} }, Orbiting, 4)

	// The ground turns to lava under a bot that is already burning.
	w.harmful[position.Floor()] = true
	bot.Advance(Tick{
		Now: c.advance(50 * time.Millisecond), Ready: true,
		Self: Self{Position: position, OnFire: true},
	}, w)

	if bot.State() != Escaping {
		t.Errorf("the bot is %v while burning in lava, want escaping first", bot.State())
	}
}

// TestDousingStopsWhenTheFireIsOut pins the end of the walk.
func TestDousingStopsWhenTheFireIsOut(t *testing.T) {
	t.Parallel()

	w := newScripted()
	circle := NewCircle(w.spawn, 25, 32)
	position := circle.At(0, 0)
	w.water[position.Add(Vec3{X: 3}).Floor()] = true

	bot, c := join(t, w, position)
	advanceTo(t, bot, w, c, func() Self { return Self{Position: position} }, Orbiting, 4)
	bot.Advance(Tick{
		Now: c.advance(50 * time.Millisecond), Ready: true,
		Self: Self{Position: position, OnFire: true},
	}, w)
	if bot.State() != Dousing {
		t.Fatalf("the bot is %v, want dousing", bot.State())
	}

	bot.Advance(Tick{
		Now: c.advance(50 * time.Millisecond), Ready: true,
		Self: Self{Position: position, OnFire: false},
	}, w)

	if bot.State() == Dousing {
		t.Error("still walking to water after the fire went out")
	}
}

// TestEscapingHeadsForTheNearestEdge pins the direction.
//
// The way out of a pool is toward its closest edge, and which edge that is
// depends on where in the pool the bot is standing. The first version of this
// walked a fixed heading, which takes half the cases deeper in -- the bot was
// photographed standing in the middle of a spreading pool having done exactly
// that.
func TestEscapingHeadsForTheNearestEdge(t *testing.T) {
	t.Parallel()

	w := newScripted()
	circle := NewCircle(w.spawn, 25, 32)
	position := circle.At(0, 0)

	bot, c := join(t, w, position)
	advanceTo(t, bot, w, c, func() Self { return Self{Position: position} }, Orbiting, 4)

	// A pool three cells wide reaching away on +X, with the bot at its -X end.
	// The near edge is one step back the way it came.
	foot := position.Floor()
	for dx := range 4 {
		w.harmful[BlockPos{X: foot.X + dx, Y: foot.Y, Z: foot.Z}] = true
	}

	action := bot.Advance(Tick{
		Now: c.advance(50 * time.Millisecond), Ready: true,
		Self: Self{Position: position}, Revision: 2,
	}, w)

	if bot.State() != Escaping {
		t.Fatalf("the bot is %v, want escaping", bot.State())
	}
	if action.Kind != StepTo {
		t.Fatalf("produced %v, want a step out", action.Kind)
	}
	// The step lands somewhere that does not hurt. The aim can be anywhere --
	// the bot re-decides next tick and the planner takes over the moment it is
	// clear -- but the place the body actually arrives this tick is the claim
	// that has to hold.
	landing := position.Toward(action.Target, DefaultBounds().Step())
	if w.Hurting(landing) {
		t.Errorf("stepped to %+v, which is still in the pool", landing)
	}
}

// TestABotAlreadyInLavaIsAllowedToMove pins the exception to the step guard.
//
// The guard refuses a step whose landing hurts, which is right for a bot on
// safe ground and fatal for one already in a pool: every way out starts with a
// step still inside it. A packet trace of a bot burning to death shows exactly
// this -- lava arrives in its cell, and it then sends nothing for a second and
// a half at a time while it declines to move.
func TestABotAlreadyInLavaIsAllowedToMove(t *testing.T) {
	t.Parallel()

	w := newScripted()
	circle := NewCircle(w.spawn, 25, 32)
	position := circle.At(0, 0)

	bot, c := join(t, w, position)
	advanceTo(t, bot, w, c, func() Self { return Self{Position: position} }, Orbiting, 4)

	// A pool wide enough that no single step leaves it.
	foot := position.Floor()
	for dx := -2; dx <= 2; dx++ {
		for dz := -2; dz <= 2; dz++ {
			w.harmful[BlockPos{X: foot.X + dx, Y: foot.Y, Z: foot.Z + dz}] = true
		}
	}

	action := bot.Advance(Tick{
		Now: c.advance(50 * time.Millisecond), Ready: true,
		Self: Self{Position: position}, Revision: 2,
	}, w)

	if bot.State() != Escaping {
		t.Fatalf("the bot is %v, want escaping", bot.State())
	}
	if action.Kind != StepTo {
		t.Fatalf("produced %v while standing in a pool, want a step: standing is what kills it", action.Kind)
	}
}

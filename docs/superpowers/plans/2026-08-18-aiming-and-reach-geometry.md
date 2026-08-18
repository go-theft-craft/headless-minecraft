# Aiming and reach geometry Implementation Plan

> **Status: complete, 2026-08-18.** The geometry landed in
> `minecraft-simulation/geom` (`57a0b56`): `AABB.Nearest` and `AABB.Reaches`,
> the example's vector arithmetic as `Vec3` methods, `Yaw`, `Pitch`, and `Look`,
> and `Behind`, `Lead`, `Tangent`, and `Away`. `examples/orbit` deleted its own
> `Vec3` and sends a computed pitch (`7cc675d`). `minecraft-simulation` v0.2.0
> is tagged, pushed, and served by the proxy, and both `headless-minecraft`
> modules require it (`d3b8a0a`).
>
> **Tasks 6 and 7 ran before task 5, which this plan told them not to do, and
> the cost was three hours of a red `main`.** The instruction was: do not start
> task 6 before task 5 has pushed a tag, because `headless-minecraft` is public
> and a `replace` directive in it is not acceptable. No `replace` was added. A
> `go.work` was used instead, which is the same hazard wearing a different hat,
> because the workspace is gitignored and makes every local gate pass against
> code no consumer can resolve.
>
> It did not stay theoretical. `go.mod` pinned `minecraft-simulation` v0.1.0,
> which had none of the six edge kinds or two `Vec3` methods `behaviour/`
> imports, and every gate in that repository's `Taskfile.yml` runs `GOWORK=off`
> on purpose — so `task test`, `task verify`, and `task build` all failed from
> the moment `behaviour` landed until the tag was cut. The gate was built to
> catch exactly this and it did. What did not happen is anyone reading it. The
> lesson is the ordering, not the tag: a release step placed before its
> consumers in a plan is load-bearing, and a workspace is not a substitute for
> it.

> **For agentic workers:** REQUIRED SUB-SKILL: Use `subagent-driven-development` (recommended) or `executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the outbound path a pitch to send and a reach test to measure legality with, and retire the duplicate `Vec3` in `examples/orbit` before four more callers copy it.

**Architecture:** The aiming functions land in `minecraft-simulation/geom` beside the vectors and boxes they extend, because they need no world view, no profile, and no entity — so they do not cost `geom` the property the module README states, that it imports nothing outside the standard library. `headless-minecraft` then consumes them through a new `minecraft-simulation` release, and `examples/orbit` deletes its own `Vec3`.

**Tech Stack:** Go 1.26.6 from `openserbia/go-flake`, Devbox, Task, `golangci-lint`. No new dependency.

Design: [aiming and reach geometry design](../specs/2026-08-18-aiming-and-reach-geometry-design.md).

## Global Constraints

- Go 1.26.6 from `openserbia/go-flake`; Devbox and Task drive every gate.
- `geom` imports nothing outside the standard library. This is a gate, not a preference: the module README states it as a property of the package.
- Angles are degrees in the protocol's own convention. Radians appear in no signature.
- Yaw is measured from south (+Z) and increases toward west (-X). Pitch is positive downward. Both match the wire and the existing `examples/orbit` implementations, which are correct and are being moved rather than rewritten.
- No version constant is typed into `geom`. Eye height and reach distance arrive as arguments.
- `devbox run -- task lint`, `devbox run -- task test`, and `devbox run -- task verify` pass before every commit.
- Conventional commit subjects. No `Co-Authored-By` trailer and no `Claude-Session` line.

## Repository span

This plan touches two repositories and a release between them. Tasks 1 through 4 are in `minecraft-simulation`, task 5 is its release, and tasks 6 and 7 are in `headless-minecraft`. Do not start task 6 before task 5 has pushed a tag: `headless-minecraft` is public and a `replace` directive in it is not acceptable.

## File Structure

| File | Repository | Responsibility |
| --- | --- | --- |
| `geom/vector.go` | simulation | Gains `HorizontalDistance`, `Toward`, `Yaw`, `Pitch`, `Look` |
| `geom/aim.go` | simulation | `Behind`, `Lead`, `Tangent` — the functions that are about a body's intent rather than about a vector |
| `geom/aabb.go` | simulation | Gains `Nearest` and `Reaches` |
| `geom/aim_test.go` | simulation | Tests for `geom/aim.go` |
| `examples/orbit/geometry.go` | headless | Loses `Vec3`, `BlockPos`, `Floor`, `Add`, `HorizontalDistance`, `Toward`, `Yaw`; keeps `Circle` and `Away` |
| `examples/orbit/send.go` | headless | `Step` sends a computed pitch |

`Behind`, `Lead`, and `Tangent` go in a new `geom/aim.go` rather than into `vector.go`, because `vector.go` is arithmetic on a vector and these three are questions a body asks. Files that change together live together, and the next aiming function will land beside these rather than in a growing `vector.go`.

---

## Task 1: The nearest point on a box, and reach

**Files:**
- Modify: `geom/aabb.go`
- Test: `geom/aabb_test.go`

**Interfaces:**
- Produces: `func (b AABB) Nearest(p Vec3) Vec3`, `func (b AABB) Reaches(eye Vec3, reach float64) bool`

This is first because everything else that measures a distance to a target measures it to this point. Reach in the game is measured to the nearest point of the target's box, not to its centre, and a client that measures to the centre refuses attacks the server would have accepted.

- [x] **Step 1: Write the failing test**

```go
func TestNearestClampsEachAxisIndependently(t *testing.T) {
	t.Parallel()

	box := AABB{MinX: 0, MinY: 0, MinZ: 0, MaxX: 1, MaxY: 2, MaxZ: 1}

	cases := []struct {
		name string
		p    Vec3
		want Vec3
	}{
		{"outside on every axis", Vec3{-5, -5, -5}, Vec3{0, 0, 0}},
		{"outside on one axis", Vec3{0.5, 5, 0.5}, Vec3{0.5, 2, 0.5}},
		{"inside returns itself", Vec3{0.5, 1, 0.5}, Vec3{0.5, 1, 0.5}},
		{"on the face", Vec3{1, 1, 0.5}, Vec3{1, 1, 0.5}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			if got := box.Nearest(c.p); got != c.want {
				t.Fatalf("Nearest(%v) = %v, want %v", c.p, got, c.want)
			}
		})
	}
}

func TestReachesComparesToTheNearestPointNotTheCentre(t *testing.T) {
	t.Parallel()

	// A box two blocks tall whose centre is 1.0 above its floor. An eye at
	// 3.5 blocks from the near face is in reach of the face and out of reach
	// of the centre, and it is the face the game measures.
	box := AABB{MinX: 0, MinY: 0, MinZ: 0, MaxX: 1, MaxY: 2, MaxZ: 1}
	eye := Vec3{X: 4.5, Y: 1, Z: 0.5}

	if !box.Reaches(eye, 3.6) {
		t.Fatal("an eye 3.5 blocks from the near face is out of reach at 3.6")
	}
	if box.Reaches(eye, 3.4) {
		t.Fatal("an eye 3.5 blocks from the near face is in reach at 3.4")
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `cd minecraft-simulation && devbox run -- go test ./geom/ -run 'TestNearest|TestReaches' -v`
Expected: FAIL, `box.Nearest undefined` and `box.Reaches undefined`.

- [x] **Step 3: Write minimal implementation**

Append to `geom/aabb.go`:

```go
// Nearest returns the point of b closest to p, which is p itself when p is
// inside b.
//
// It is the point every reach check measures to. The game measures reach to a
// target's box rather than to its centre, so a client that measures to the
// centre refuses attacks the server would have accepted, and the taller the
// target the wider the disagreement.
func (b AABB) Nearest(p Vec3) Vec3 {
	return Vec3{
		X: clamp(p.X, b.MinX, b.MaxX),
		Y: clamp(p.Y, b.MinY, b.MaxY),
		Z: clamp(p.Z, b.MinZ, b.MaxZ),
	}
}

// Reaches reports whether eye is within reach of b's nearest point.
//
// The reach distance is an argument because the two versions disagree about
// it and because it differs by what is being reached for. Nothing here asserts
// a value; M9.5 and M9.6 measure them.
func (b AABB) Reaches(eye Vec3, reach float64) bool {
	d := b.Nearest(eye).Sub(eye)

	return d.X*d.X+d.Y*d.Y+d.Z*d.Z <= reach*reach
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}

	return v
}
```

- [x] **Step 4: Run test to verify it passes**

Run: `cd minecraft-simulation && devbox run -- go test ./geom/ -run 'TestNearest|TestReaches' -v`
Expected: PASS.

- [x] **Step 5: Add the property test**

```go
func TestNearestAlwaysLandsInsideTheBox(t *testing.T) {
	t.Parallel()

	box := AABB{MinX: -2, MinY: 0, MinZ: -3, MaxX: 5, MaxY: 4, MaxZ: 1}
	rng := rand.New(rand.NewPCG(1, 2))

	for range 10000 {
		p := Vec3{
			X: rng.Float64()*40 - 20,
			Y: rng.Float64()*40 - 20,
			Z: rng.Float64()*40 - 20,
		}
		n := box.Nearest(p)

		if n.X < box.MinX || n.X > box.MaxX ||
			n.Y < box.MinY || n.Y > box.MaxY ||
			n.Z < box.MinZ || n.Z > box.MaxZ {
			t.Fatalf("Nearest(%v) = %v, outside the box", p, n)
		}
	}
}
```

- [x] **Step 6: Run the package suite**

Run: `cd minecraft-simulation && devbox run -- task test`
Expected: PASS.

- [x] **Step 7: Commit**

```bash
git add geom/aabb.go geom/aabb_test.go
git commit -m "feat(geom): measure reach to a box's nearest point"
```

---

## Task 2: Move the example's vector arithmetic into geom

**Files:**
- Modify: `geom/vector.go`
- Test: `geom/vector_test.go`

**Interfaces:**
- Produces: `func (v Vec3) HorizontalDistance(o Vec3) float64`, `func (v Vec3) Toward(o Vec3, limit float64) Vec3`, `func (v Vec3) Yaw(o Vec3) float32`

These three exist and are correct in `examples/orbit/geometry.go`. This task moves them, with their doc comments, because the reasoning in those comments was paid for by a live run and must not be lost. `Floor` and `Add` already exist in `geom` and are not moved.

- [x] **Step 1: Write the failing test**

```go
func TestYawIsMeasuredFromSouthTowardWest(t *testing.T) {
	t.Parallel()

	origin := Vec3{}

	cases := []struct {
		name string
		to   Vec3
		want float32
	}{
		{"south is zero", Vec3{Z: 1}, 0},
		{"west is ninety", Vec3{X: -1}, 90},
		{"north is one eighty", Vec3{Z: -1}, 180},
		{"east is minus ninety", Vec3{X: 1}, -90},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			if got := origin.Yaw(c.to); math.Abs(float64(got-c.want)) > 1e-4 {
				t.Fatalf("Yaw(%v) = %v, want %v", c.to, got, c.want)
			}
		})
	}
}

func TestTowardStopsExactlyOnTheTarget(t *testing.T) {
	t.Parallel()

	from := Vec3{X: 0, Y: 64, Z: 0}
	to := Vec3{X: 3, Y: 70, Z: 4}

	// Five blocks away horizontally, stepping ten: it arrives rather than
	// overshooting, and it keeps its own Y because this chooses no height.
	got := from.Toward(to, 10)
	want := Vec3{X: 3, Y: 64, Z: 4}

	if got != want {
		t.Fatalf("Toward = %v, want %v", got, want)
	}
}

func TestTowardAtZeroDistanceDoesNotDivideByZero(t *testing.T) {
	t.Parallel()

	at := Vec3{X: 1, Y: 2, Z: 3}

	if got := at.Toward(at, 0.5); got != at {
		t.Fatalf("Toward onto itself = %v, want %v", got, at)
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `cd minecraft-simulation && devbox run -- go test ./geom/ -run 'TestYaw|TestToward' -v`
Expected: FAIL, `Yaw undefined` and `Toward undefined`.

- [x] **Step 3: Write the implementation**

Copy `HorizontalDistance`, `Toward`, and `Yaw` verbatim from `headless-minecraft/examples/orbit/geometry.go` into `geom/vector.go`, keeping their doc comments unchanged. The bodies are already correct; only the package and the receiver type change, and `geom.Vec3` has the same three fields.

- [x] **Step 4: Run test to verify it passes**

Run: `cd minecraft-simulation && devbox run -- go test ./geom/ -run 'TestYaw|TestToward' -v`
Expected: PASS.

- [x] **Step 5: Commit**

```bash
git add geom/vector.go geom/vector_test.go
git commit -m "feat(geom): add horizontal distance, Toward, and Yaw"
```

---

## Task 3: Pitch and Look

**Files:**
- Modify: `geom/vector.go`
- Test: `geom/vector_test.go`

**Interfaces:**
- Consumes: `Yaw` and `HorizontalDistance` from task 2.
- Produces: `func (v Vec3) Pitch(o Vec3) float32`, `func (v Vec3) Look(o Vec3) (yaw, pitch float32)`

This is the task the whole design exists for. `examples/orbit/send.go` sends `Pitch: 0` as a literal, so the bot cannot look at anything above or below its own eyes, and every aimed primitive is blocked behind it.

- [x] **Step 1: Write the failing test**

```go
func TestPitchIsPositiveDownward(t *testing.T) {
	t.Parallel()

	eye := Vec3{X: 0, Y: 10, Z: 0}

	cases := []struct {
		name string
		to   Vec3
		want float32
	}{
		{"straight down is ninety", Vec3{X: 0, Y: 0, Z: 0}, 90},
		{"straight up is minus ninety", Vec3{X: 0, Y: 20, Z: 0}, -90},
		{"level is zero", Vec3{X: 0, Y: 10, Z: 5}, 0},
		{"a forty-five degree descent", Vec3{X: 0, Y: 5, Z: 5}, 45},
		{"a forty-five degree climb", Vec3{X: 0, Y: 15, Z: 5}, -45},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			if got := eye.Pitch(c.to); math.Abs(float64(got-c.want)) > 1e-4 {
				t.Fatalf("Pitch(%v) = %v, want %v", c.to, got, c.want)
			}
		})
	}
}

func TestLookAgreesWithYawAndPitchSeparately(t *testing.T) {
	t.Parallel()

	rng := rand.New(rand.NewPCG(3, 4))

	for range 1000 {
		from := Vec3{rng.Float64() * 100, rng.Float64() * 100, rng.Float64() * 100}
		to := Vec3{rng.Float64() * 100, rng.Float64() * 100, rng.Float64() * 100}

		yaw, pitch := from.Look(to)

		if yaw != from.Yaw(to) || pitch != from.Pitch(to) {
			t.Fatalf("Look = (%v, %v), want (%v, %v)", yaw, pitch, from.Yaw(to), from.Pitch(to))
		}
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `cd minecraft-simulation && devbox run -- go test ./geom/ -run 'TestPitch|TestLook' -v`
Expected: FAIL, `Pitch undefined`.

- [x] **Step 3: Write minimal implementation**

```go
// Pitch returns the angle from v to o in degrees, as the wire carries it.
//
// Positive is down. That is the game's convention and not an arbitrary choice
// here: a client that flips the sign looks at the sky when it means to mine
// the block under its feet, and every aimed action it sends misses in a way
// that is hard to see from the packets alone.
//
// The horizontal leg is a distance rather than a signed offset, so a target
// behind the eye pitches the same as one in front. Turning to face it is Yaw's
// job.
func (v Vec3) Pitch(o Vec3) float32 {
	return float32(math.Atan2(-(o.Y - v.Y), v.HorizontalDistance(o)) * 180 / math.Pi)
}

// Look returns the yaw and pitch from v to o.
//
// Every caller that needs one needs the other: an outbound look carries both
// fields and a caller that computed them separately would walk the same vector
// twice for a value it already had.
func (v Vec3) Look(o Vec3) (yaw, pitch float32) {
	return v.Yaw(o), v.Pitch(o)
}
```

- [x] **Step 4: Run test to verify it passes**

Run: `cd minecraft-simulation && devbox run -- go test ./geom/ -run 'TestPitch|TestLook' -v`
Expected: PASS.

- [x] **Step 5: Commit**

```bash
git add geom/vector.go geom/vector_test.go
git commit -m "feat(geom): add Pitch and Look"
```

---

## Task 4: Behind, Lead, and Tangent

**Files:**
- Create: `geom/aim.go`
- Test: `geom/aim_test.go`

**Interfaces:**
- Consumes: `Vec3` arithmetic from task 2.
- Produces: `func Behind(target, facing Vec3, d float64) Vec3`, `func Lead(target, velocity Vec3, ticks float64) Vec3`, `func Tangent(centre, here Vec3, clockwise bool) Vec3`, `func Away(here, threat Vec3, distance float64) Vec3`

These are functions rather than methods because none of them is a question about one vector. `Behind` takes a target and a facing, `Lead` takes a target and a velocity, `Tangent` takes a centre and a position, and `Away` takes a position and a threat.

`Away` already exists in `examples/orbit/geometry.go` and moves here verbatim with its doc comment, for the same reason `Toward` did in task 2: `behaviour.Flee` is its second caller, and a second copy is what this plan exists to prevent.

- [x] **Step 1: Write the failing test**

```go
func TestBehindIsOppositeTheFacing(t *testing.T) {
	t.Parallel()

	target := Vec3{X: 10, Y: 64, Z: 10}
	facing := Vec3{X: 0, Y: 0, Z: 1} // facing south

	got := Behind(target, facing, 3)
	want := Vec3{X: 10, Y: 64, Z: 7}

	if got != want {
		t.Fatalf("Behind = %v, want %v", got, want)
	}
}

func TestBehindIgnoresTheFacingMagnitude(t *testing.T) {
	t.Parallel()

	target := Vec3{X: 0, Y: 0, Z: 0}

	unit := Behind(target, Vec3{X: 0, Y: 0, Z: 1}, 2)
	long := Behind(target, Vec3{X: 0, Y: 0, Z: 17}, 2)

	if unit != long {
		t.Fatalf("Behind with a long facing = %v, want %v", long, unit)
	}
}

func TestBehindAZeroFacingReturnsTheTarget(t *testing.T) {
	t.Parallel()

	target := Vec3{X: 1, Y: 2, Z: 3}

	if got := Behind(target, Vec3{}, 5); got != target {
		t.Fatalf("Behind with no facing = %v, want %v", got, target)
	}
}

func TestLeadProjectsTheTargetForward(t *testing.T) {
	t.Parallel()

	target := Vec3{X: 0, Y: 64, Z: 0}
	velocity := Vec3{X: 0.2, Y: 0, Z: -0.1}

	got := Lead(target, velocity, 10)
	want := Vec3{X: 2, Y: 64, Z: -1}

	if got != want {
		t.Fatalf("Lead = %v, want %v", got, want)
	}
}

func TestLeadWithNoVelocityReturnsTheTarget(t *testing.T) {
	t.Parallel()

	target := Vec3{X: 1, Y: 2, Z: 3}

	if got := Lead(target, Vec3{}, 40); got != target {
		t.Fatalf("Lead with no velocity = %v, want %v", got, target)
	}
}

func TestTangentIsPerpendicularToTheRadius(t *testing.T) {
	t.Parallel()

	centre := Vec3{X: 0, Y: 64, Z: 0}
	here := Vec3{X: 5, Y: 64, Z: 0}

	got := Tangent(centre, here, true)

	// Perpendicular in the horizontal plane: the dot product of the tangent
	// and the radius is zero.
	radius := here.Sub(centre)
	if dot := got.X*radius.X + got.Z*radius.Z; math.Abs(dot) > 1e-9 {
		t.Fatalf("tangent %v is not perpendicular to radius %v, dot = %v", got, radius, dot)
	}
	if length := math.Hypot(got.X, got.Z); math.Abs(length-1) > 1e-9 {
		t.Fatalf("tangent %v has length %v, want 1", got, length)
	}
}

func TestTangentReversesWithDirection(t *testing.T) {
	t.Parallel()

	centre := Vec3{X: 0, Y: 0, Z: 0}
	here := Vec3{X: 5, Y: 0, Z: 0}

	cw := Tangent(centre, here, true)
	ccw := Tangent(centre, here, false)

	if cw.X != -ccw.X || cw.Z != -ccw.Z {
		t.Fatalf("clockwise %v and counter-clockwise %v are not opposites", cw, ccw)
	}
}

func TestTangentAtTheCentreIsZero(t *testing.T) {
	t.Parallel()

	centre := Vec3{X: 1, Y: 2, Z: 3}

	if got := Tangent(centre, centre, true); got != (Vec3{}) {
		t.Fatalf("Tangent at the centre = %v, want the zero vector", got)
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `cd minecraft-simulation && devbox run -- go test ./geom/ -run 'TestBehind|TestLead|TestTangent' -v`
Expected: FAIL, undefined.

- [x] **Step 3: Write minimal implementation**

```go
// Package-internal note: these three answer questions a body asks about a
// target, which is why they are here rather than in vector.go. That file is
// arithmetic on a vector and stays that way.

package geom

import "math"

// Behind returns the position d blocks behind target, given the direction it
// faces.
//
// The facing is normalized horizontally, so a caller may pass a velocity, a
// look vector, or a difference between two positions without scaling it first.
// A facing with no horizontal component leaves nothing to be behind, and the
// target is returned unchanged rather than a position divided by zero.
func Behind(target, facing Vec3, d float64) Vec3 {
	length := math.Hypot(facing.X, facing.Z)
	if length == 0 {
		return target
	}

	return Vec3{
		X: target.X - facing.X/length*d,
		Y: target.Y,
		Z: target.Z - facing.Z/length*d,
	}
}

// Lead returns where a mover travelling at velocity will be in ticks.
//
// The caller supplies the velocity. Nothing here tracks an entity or reads a
// snapshot: this is arithmetic, and how a caller learns how fast something is
// moving is the caller's business.
//
// It projects in a straight line and models no gravity and no drag. For the
// handful of ticks a lead is worth computing over, that is what a mover on the
// ground does, and a caller aiming at something falling wants the movement
// kernel rather than this.
func Lead(target, velocity Vec3, ticks float64) Vec3 {
	return Vec3{
		X: target.X + velocity.X*ticks,
		Y: target.Y + velocity.Y*ticks,
		Z: target.Z + velocity.Z*ticks,
	}
}

// Tangent returns the unit heading that circles centre from here.
//
// It is what a caller strafes along. The orbit example walks a ring of
// waypoints instead, which is a fine way to describe a fixed circle and a poor
// way to circle something that moves; this needs no ring and no rebuild when
// the centre changes.
//
// At the centre there is no radius and therefore no tangent, and the zero
// vector is returned rather than a heading divided by zero.
func Tangent(centre, here Vec3, clockwise bool) Vec3 {
	dx, dz := here.X-centre.X, here.Z-centre.Z

	length := math.Hypot(dx, dz)
	if length == 0 {
		return Vec3{}
	}

	dx, dz = dx/length, dz/length

	if clockwise {
		return Vec3{X: -dz, Z: dx}
	}

	return Vec3{X: dz, Z: -dx}
}
```

- [x] **Step 4: Run test to verify it passes**

Run: `cd minecraft-simulation && devbox run -- go test ./geom/ -run 'TestBehind|TestLead|TestTangent' -v`
Expected: PASS.

- [x] **Step 5: Move Away**

Copy `Away` verbatim from `headless-minecraft/examples/orbit/geometry.go` into `geom/aim.go`, keeping its doc comment, and add the case that pins its direction:

```go
func TestAwayGoesOppositeTheThreat(t *testing.T) {
	t.Parallel()

	here := Vec3{X: 0, Y: 64, Z: 0}
	threat := Vec3{X: 5, Y: 64, Z: 0}

	got := Away(here, threat, 10)

	if got.X >= here.X {
		t.Fatalf("Away = %v, which is toward a threat at x=5 from x=0", got)
	}
}
```

- [x] **Step 6: Verify geom still imports only the standard library**

Run: `cd minecraft-simulation && devbox run -- go list -deps ./geom | grep -v '^\(internal/\|[a-z]*$\|[a-z]*/\)' | grep -v 'go-theft-craft' || echo "stdlib only"`
Expected: no `github.com/` line for any third-party module.

- [x] **Step 7: Run every gate**

Run: `cd minecraft-simulation && devbox run -- task verify`
Expected: PASS.

- [x] **Step 8: Commit**

```bash
git add geom/aim.go geom/aim_test.go
git commit -m "feat(geom): add Behind, Lead, Tangent, and Away"
```

---

## Task 5: Release minecraft-simulation

**Files:**
- Modify: `CHANGELOG.md`

**Interfaces:**
- Produces: a pushed `v0.2.0` tag that `headless-minecraft` can require.

`headless-minecraft` is public and a `replace` directive in it is not acceptable, which the navigation plan of 2026-08-17 records. Tasks 6 and 7 cannot start until this one has pushed.

- [x] **Step 1: Add the changelog entry**

Under a new `## v0.2.0` heading, in the style the existing entries use:

```markdown
### Added

- `geom.AABB.Nearest` and `geom.AABB.Reaches`, so reach is measured to a box's
  nearest point rather than to its centre.
- `geom.Vec3.HorizontalDistance`, `Toward`, and `Yaw`, moved from
  `headless-minecraft/examples/orbit` where they were the only copy.
- `geom.Vec3.Pitch` and `geom.Vec3.Look`.
- `geom.Behind`, `geom.Lead`, `geom.Tangent`, and `geom.Away`, the last
  moved from `headless-minecraft/examples/orbit`.
```

- [x] **Step 2: Run the release gate**

Run: `cd minecraft-simulation && devbox run -- task release:check`
Expected: PASS.

- [x] **Step 3: Commit and tag**

```bash
git add CHANGELOG.md
git commit -m "docs: prepare the 0.2.0 release"
git tag v0.2.0
git push origin main --tags
```

- [x] **Step 4: Confirm the module proxy serves it**

Run: `cd minecraft-simulation && GOPROXY=https://proxy.golang.org go list -m github.com/go-theft-craft/minecraft-simulation@v0.2.0`
Expected: the version prints. If it does not, wait and retry; do not proceed to task 6 until it does.

---

## Task 6: Retire the example's Vec3

**Files:**
- Modify: `headless-minecraft/examples/go.mod`
- Modify: `headless-minecraft/examples/orbit/geometry.go`
- Modify: `headless-minecraft/examples/orbit/geometry_test.go`
- Modify: `headless-minecraft/examples/orbit/bot.go`, `bounds.go`, `observed.go`, `route.go`, `send.go`, `main.go` as the compiler requires
- Test: the existing `geometry_test.go`, `world_test.go`, `send_test.go`

**Interfaces:**
- Consumes: `geom.Vec3`, `geom.Behind`, `geom.Lead`, `geom.Tangent` from tasks 2 through 4.
- Produces: an `examples/orbit` that defines no `Vec3`.

`geom.Floor` returns a `geom.BlockPos` whose fields are `int32`, while the example's `BlockPos` uses `int`. That difference will surface at every call site and is the reason this is its own task rather than a step in another.

- [x] **Step 1: Bump the dependency**

```bash
cd headless-minecraft/examples
devbox run -- go get github.com/go-theft-craft/minecraft-simulation@v0.2.0
```

- [x] **Step 2: Run the tests to see them pass before the change**

Run: `cd headless-minecraft && devbox run -- task examples`
Expected: PASS. This is the baseline the rewrite must not move.

- [x] **Step 3: Delete the duplicated declarations**

From `examples/orbit/geometry.go`, delete `Vec3`, `BlockPos`, `Floor`, `Add`, `HorizontalDistance`, `Toward`, `Yaw`, and `Away`. Keep `Circle`, `NewCircle`, `At`, `angle`, `Nearest`, and `Deviation`: a waypoint ring is what that bot does, not a fact about the game.

`Away` moves to `geom` with the others rather than staying here, because `behaviour.Flee` needs it — see the [composed behaviours plan](2026-08-18-composed-behaviours.md) task 2. A second copy of it in that package is the duplicate this task exists to stop.

Add the import and a file-local alias so the diff at the call sites stays small:

```go
import "github.com/go-theft-craft/minecraft-simulation/geom"

// Vec3 is geom's, aliased so this example reads the way it did. The example
// carried its own copy until 2026-08-18; the copy is gone and this name is
// kept because the alternative is a rename in every file at once.
type Vec3 = geom.Vec3
```

- [x] **Step 4: Fix the BlockPos width**

Every place the example compared or stored a `BlockPos` field as `int` now sees `int32`. Change the local declarations rather than converting at the call sites: a conversion at each use is where an off-by-one hides.

- [x] **Step 5: Run the tests**

Run: `cd headless-minecraft && devbox run -- task examples`
Expected: PASS, with the same behaviour as step 2.

- [x] **Step 6: Verify the deletion**

Run: `cd headless-minecraft && grep -n 'type Vec3\|func (v Vec3) Yaw\|func (v Vec3) Toward' examples/orbit/geometry.go`
Expected: only the `type Vec3 =` alias line.

- [x] **Step 7: Commit**

```bash
git add examples/
git commit -m "refactor(orbit): use geom's vectors instead of a copy"
```

---

## Task 7: Send a computed pitch

**Files:**
- Modify: `headless-minecraft/examples/orbit/send.go`
- Test: `headless-minecraft/examples/orbit/send_test.go`

**Interfaces:**
- Consumes: `geom.Vec3.Look` from task 3.
- Produces: a `Sender.Step` that sends a computed pitch.

- [x] **Step 1: Write the failing test**

```go
func TestStepSendsAComputedPitch(t *testing.T) {
	t.Parallel()

	// The bot walks level, so the pitch it sends while walking is zero — but
	// it must be zero because it was computed, not because it was typed. A
	// target below the bot proves the difference.
	client := &recordingClient{}
	sender := NewSender(client, testBounds())

	from := Vec3{X: 0, Y: 70, Z: 0}
	target := Vec3{X: 0, Y: 64, Z: 10}

	if _, err := sender.Step(t.Context(), from, target, false); err != nil {
		t.Fatalf("Step: %v", err)
	}

	action, ok := client.last().(version.ActionMoveLook)
	if !ok {
		t.Fatalf("sent %T, want ActionMoveLook", client.last())
	}
	if action.Pitch <= 0 {
		t.Fatalf("Pitch = %v, want a positive angle toward a target six blocks below", action.Pitch)
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `cd headless-minecraft && devbox run -- go test ./examples/orbit/ -run TestStepSendsAComputedPitch -v`
Expected: FAIL, `Pitch = 0`.

- [x] **Step 3: Write minimal implementation**

In `Sender.Step`, replace the yaw-only call and the `Pitch: 0` literal:

```go
	yaw, pitch := from.Look(target)

	err := s.client.Do(ctx, version.ActionMoveLook{
		X: next.X, Y: next.Y, Z: next.Z,
		Yaw: yaw, Pitch: pitch,
		OnGround: true,
	})
```

Replace the doc comment's paragraph about `Pitch: 0` with what is now true: the pitch is computed toward the target, and a bot walking a level circle sends zero because the target is level, not because nothing else could be sent.

- [x] **Step 4: Run test to verify it passes**

Run: `cd headless-minecraft && devbox run -- go test ./examples/orbit/ -run TestStepSendsAComputedPitch -v`
Expected: PASS.

- [x] **Step 5: Confirm the orbit behaviour is unchanged**

Run: `cd headless-minecraft && devbox run -- task examples`
Expected: PASS. The circle is level, so every pitch on it is zero and the bot's observable behaviour is the same as before.

- [x] **Step 6: Run every gate in both repositories**

Run: `cd headless-minecraft && devbox run -- task verify`
Expected: PASS.

- [x] **Step 7: Commit**

```bash
git add examples/orbit/send.go examples/orbit/send_test.go
git commit -m "feat(orbit): send a computed pitch instead of zero"
```

---

## Definition of done

- `Sender.Step` sends a computed pitch rather than the literal `0`.
- `examples/orbit/geometry.go` declares no `Vec3` struct and no `Add`, `Floor`, `HorizontalDistance`, `Toward`, `Yaw`, or `Away`.
- `geom` imports nothing outside the standard library, proved by the check in task 4.
- Every new function has a test that fails if its sign convention is flipped: `Yaw`'s four cardinals, `Pitch`'s five cases, and `Tangent`'s reversal.
- `minecraft-simulation` v0.2.0 is tagged, pushed, and served by the module proxy.
- `devbox run -- task verify` passes in both repositories.

## Risks

| Risk | Mitigation |
| --- | --- |
| The `int` to `int32` change on `BlockPos` hides an off-by-one | Task 6 changes the local declarations rather than converting at call sites, and the existing `geometry_test.go` and `world_test.go` are the baseline |
| A released `v0.2.0` cannot be unreleased | The module mirror serves immutable snapshots. Task 5 runs `release:check` before tagging, and tasks 1 through 4 are all gated before it |
| The pitch sign is wrong and nothing catches it | The bot walks level, so the orbit example would not notice. `TestPitchIsPositiveDownward` is the gate, and it tests a target below the eye specifically because the example cannot |

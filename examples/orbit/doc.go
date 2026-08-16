// Command orbit jumps its way around a circle of radius 25 centred on spawn,
// steps around what blocks it, fights back when something hits it, respawns and
// returns to the circle when it dies, and stands still when it is sealed in.
//
// The design is in docs/superpowers/specs/2026-08-16-orbit-example-design.md.
//
// # What runs today
//
// The decision core runs and is tested. [Bot.Advance] is a pure function from
// one [Tick] and a [World] to one [Action]: it owns the orbit geometry, the
// bypass search, retaliation, the respawn path, and every bound. It needs no
// server, which is why its tests script a world instead of connecting to one.
//
// The shell does not. Driving the core against a real server needs observed
// world state (M7) and the movement, attack, and respawn actions (M9), and
// neither exists yet. [Pending] stands in for both and reports which milestone
// owes what, so `go run ./orbit` against a live server connects, reaches play,
// and then stops with that list rather than pretending to orbit.
//
// The seam between the two is [World] and [Actuator], and it is deliberately
// narrow: when M7 and M9 land, this example gains two adapters and the core
// does not change.
package main

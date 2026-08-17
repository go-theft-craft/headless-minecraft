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
// Observation runs too. [Observed] implements [World] over one
// world.Snapshot, so the bot sees the spawn it orbits, the player it moves,
// and the entities it may fight. Accepting it changed nothing in the core, which
// is what the seam was for.
//
// Solidity is answered too. The library reports the block state the server sent
// and models no block semantics, which is right, so [Chunk47Solidity] reads a
// table extracted from the game itself: vanilla decides passability with
// !blockMaterial.blocksMovement() and its ground navigator uses that same
// predicate, so this does too.
//
// What is still owed is attack, which is M9.6.
//
// The seam is [World], [Solidity], and [Actuator], and it is deliberately
// narrow: each is one type to write when what it needs exists.
package main

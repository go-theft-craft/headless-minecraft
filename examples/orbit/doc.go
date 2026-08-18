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
// routing, escape, the respawn path, and every bound. It needs no
// server, which is why its tests script a world instead of connecting to one.
//
// Observation runs too. [Observed] implements [World] over one
// world.Snapshot, so the bot sees the spawn it orbits, the player it moves,
// and the entities it may fight. Accepting it changed nothing in the core, which
// is what the seam was for.
//
// Solidity is answered too, and no longer from here. The world package reports
// the block state the server sent and models no block semantics, which is
// right; the mapping from a state to "solid" belongs to minecraft-protocol,
// which measures it from the game itself. [MeasuredSolidity] is the whole of
// what this example does with it. Vanilla decides passability with
// !blockMaterial.blocksMovement() and its ground navigator uses that same
// predicate, so the measurement does too.
//
// Only protocol 47 has been measured. On the current protocol the bot sees the
// world, classifies nothing in it, and refuses to move — which it reports on
// startup rather than after standing still.
//
// The bot runs from whatever hits it rather than hitting back, so attack --
// which is M9.6, and unfinished -- is not on its path. The port for it stays
// declared and unimplemented.
//
// The seam is [World], [Solidity], and [Actuator], and it is deliberately
// narrow: each is one type to write when what it needs exists.
package main

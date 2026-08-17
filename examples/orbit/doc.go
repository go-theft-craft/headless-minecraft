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
// Two things still stop a revolution, and the program says both before it
// connects. The actions are owed by milestones — movement is M8.8, attack is
// M9.6, and respawn has no primitive planned — so [Pending] stands in for
// [Actuator] and names the milestone rather than failing silently. Block
// solidity is owed by nobody: the library reports the block state the server
// sent and models no block semantics, so nothing maps a state to whether it
// stops the bot. [PendingSolidity] classifies nothing and every position
// therefore reads [Unknown], so the bypass search accepts no offset and the bot
// will report itself trapped as soon as it can move at all. The design argues
// that case; the short version is that both available approximations are wrong
// in ways an automated check would not catch.
//
// The seam is [World], [Solidity], and [Actuator], and it is deliberately
// narrow: each is one type to write when what it needs exists.
package main

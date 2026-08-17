package event

// The world domain covers the terrain the server streamed and the scalars that
// describe the world around it. Chunks and environment are separate reducers;
// they share this domain because a subscriber selects by domain, not by file.

// BlockPosition is one block's coordinates in an event.
type BlockPosition struct{ X, Y, Z int32 }

// WorldChunkLoaded reports a column the server sent. Its sections are stored
// as received and decoded on first block read.
type WorldChunkLoaded struct {
	Stamp

	X, Z     int32
	Sections int
}

func (WorldChunkLoaded) Name() Name     { return NameWorldChunkLoaded }
func (WorldChunkLoaded) Domain() Domain { return DomainWorld }

// WorldChunkUnloaded reports a column released.
type WorldChunkUnloaded struct {
	Stamp

	X, Z int32
}

func (WorldChunkUnloaded) Name() Name     { return NameWorldChunkUnloaded }
func (WorldChunkUnloaded) Domain() Domain { return DomainWorld }

// WorldBlocksChanged reports blocks the server changed.
//
// One packet changes one block and another changes many, and both are the same
// fact, so this carries a set and a single change is a set of one. Dropped
// counts changes that landed in a chunk this client has not loaded or a
// section it cannot decode.
type WorldBlocksChanged struct {
	Stamp

	Positions []BlockPosition
	States    []uint32
	Dropped   int
}

func (WorldBlocksChanged) Name() Name     { return NameWorldBlocksChanged }
func (WorldBlocksChanged) Domain() Domain { return DomainWorld }

// WorldBlockEntityChanged reports a block entity the server sent.
type WorldBlockEntityChanged struct {
	Stamp

	X, Y, Z int32
}

func (WorldBlockEntityChanged) Name() Name     { return NameWorldBlockEntityChanged }
func (WorldBlockEntityChanged) Domain() Domain { return DomainWorld }

// WorldLightChanged reports light data for a column.
type WorldLightChanged struct {
	Stamp

	X, Z int32
}

func (WorldLightChanged) Name() Name     { return NameWorldLightChanged }
func (WorldLightChanged) Domain() Domain { return DomainWorld }

// WorldTimeChanged reports the world clock moving.
//
// Age is the world's total age in ticks and both protocols send it. The
// time of day is where they part: protocol 47 sends one number, and 26.1
// replaced it with a set of clocks. TimeOfDayKnown says whether this event
// carries one, and Clocks carries what 775 actually sent.
type WorldTimeChanged struct {
	Stamp

	Age            int64
	TimeOfDay      int64
	TimeOfDayKnown bool
	Clocks         []Clock
}

func (WorldTimeChanged) Name() Name     { return NameWorldTimeChanged }
func (WorldTimeChanged) Domain() Domain { return DomainWorld }

// Clock is one of the server's time-of-day clocks, as protocol 775 sends them.
// The ID space is the server's own and is not interpreted here.
type Clock struct {
	ID          int32
	TotalTicks  int64
	PartialTick float32
	Rate        float32
}

// WorldBorderChanged reports the border after a change, not the change.
//
// Protocol 47 sends one packet with an action discriminator and 775 sends six
// distinct packets. Each one updates part of the border, and every one of the
// eleven produces this event carrying the whole resulting border, so a
// subscriber never has to remember which fields the last packet touched.
type WorldBorderChanged struct {
	Stamp

	Border Border
}

func (WorldBorderChanged) Name() Name     { return NameWorldBorderChanged }
func (WorldBorderChanged) Domain() Domain { return DomainWorld }

// Border is the world border's state.
//
// Diameter, not radius: protocol 47's schema calls the field `radius` and
// sends the length of a side, which is what 775 renamed to `diameter`. They are
// the same quantity under two names, and carrying 47's name would halve the
// border for anybody who believed it.
type Border struct {
	X, Z float64
	// OldDiameter and NewDiameter are the ends of a move. A border that is not
	// moving reports the same value for both.
	OldDiameter, NewDiameter float64
	// Speed is how long the move takes, in milliseconds, and zero means the
	// change was immediate.
	Speed int64
	// PortalBoundary is the coordinate limit the nether portal calculation
	// clamps to. Only the initializing packet carries it.
	PortalBoundary int32
	WarningTime    int32
	WarningBlocks  int32
	// Known reports whether the server has described the border at all. A
	// world with no border packet and a border of diameter zero are different.
	Known bool
}

// WorldWeatherChanged reports rain or thunder starting, stopping, or changing
// intensity.
//
// Both protocols carry this on the game-state packet the player domain also
// reads, discriminated by a reason. **The two protocols number the reasons
// oppositely**: on protocol 47, 1 ends rain and 2 begins it, and on 775 the
// mapper names 1 `start_raining` and 2 `stop_raining`. Each adapter resolves
// its own, and this event reports the resolved fact.
type WorldWeatherChanged struct {
	Stamp

	Raining bool
	// RainLevel and ThunderLevel are the server's own fade values. Protocol 47
	// sends no thunder level at all, so it stays zero there.
	RainLevel    float32
	ThunderLevel float32
}

func (WorldWeatherChanged) Name() Name     { return NameWorldWeatherChanged }
func (WorldWeatherChanged) Domain() Domain { return DomainWorld }

// WorldDifficultyChanged reports the difficulty the server set.
//
// Difficulty is a name on 775 and a number on protocol 47, and both become the
// name, because a number is meaningless to a caller and the mapping is fixed.
// Locked is 775 only; protocol 47 does not send it.
type WorldDifficultyChanged struct {
	Stamp

	Difficulty string
	Locked     bool
}

func (WorldDifficultyChanged) Name() Name     { return NameWorldDifficultyChanged }
func (WorldDifficultyChanged) Domain() Domain { return DomainWorld }

// WorldExplosionOccurred reports an explosion.
//
// It carries no block list. Protocol 47 sends the destroyed blocks as offsets
// and 775 stopped sending them at all, sending particles instead, and the
// blocks that actually changed arrive as block changes either way. Reporting a
// block list from one protocol and not the other would invite a caller to rely
// on it.
type WorldExplosionOccurred struct {
	Stamp

	X, Y, Z float64
	Radius  float32
	// Knockback is the velocity the server applied to the local player, and
	// Knocked reports whether it sent any: 775 makes it optional.
	KnockbackX, KnockbackY, KnockbackZ float64
	Knocked                            bool
}

func (WorldExplosionOccurred) Name() Name     { return NameWorldExplosionOccurred }
func (WorldExplosionOccurred) Domain() Domain { return DomainWorld }

// WorldEventOccurred reports a discrete effect at a position — a door opening,
// a dispenser firing, a portal being lit.
//
// Particles are not this. Both protocols send a particle packet from the same
// area of the schema, and particles are presentational: they carry no state,
// nothing later reads them, and folding them in here would make a subscriber
// that acts on world events fire on smoke.
type WorldEventOccurred struct {
	Stamp

	EffectID int32
	Position BlockPosition
	Data     int32
	Global   bool
}

func (WorldEventOccurred) Name() Name     { return NameWorldEventOccurred }
func (WorldEventOccurred) Domain() Domain { return DomainWorld }

// WorldSimulationSettingsChanged reports the distances, tick rate, and game
// rules the server runs the world under.
//
// Game rules ride this event because the taxonomy declares no name of their
// own and a game rule is a simulation setting — whether the daylight cycle
// runs, whether mobs spawn. RuleKeys names the rules this event changed, in the
// same shape EntityAttributesChanged uses, and the snapshot holds the values.
//
// Every field here is protocol 775 only. Protocol 47 sends no view distance, no
// simulation distance, no view centre, no tick rate, and no game rules, so this
// event never fires on 47 and the snapshot says the values are unknown rather
// than reporting zeroes.
type WorldSimulationSettingsChanged struct {
	Stamp

	ViewDistance       int32
	SimulationDistance int32
	ViewChunkX         int32
	ViewChunkZ         int32
	TickRate           float32
	Frozen             bool
	RuleKeys           []string
}

func (WorldSimulationSettingsChanged) Name() Name     { return NameWorldSimulationSettingsChanged }
func (WorldSimulationSettingsChanged) Domain() Domain { return DomainWorld }

// WorldSpawnChanged reports the spawn position the server sent.
//
// This is the compass target, and calling it "the world spawn" is only true
// until the player sleeps: a vanilla server sends the level's shared spawn on
// join and re-sends this packet whenever the player's own respawn point moves,
// so a caller that treats it as a fixed landmark is right until the first bed
// and wrong afterwards. Nothing here distinguishes the two, because the packet
// does not: it carries a position and no reason for it.
//
// Both protocols send it. Protocol 775 adds the dimension it is in and the
// angle to face on arrival; protocol 47 sends coordinates alone, and Angled is
// false for the whole of such a session.
type WorldSpawnChanged struct {
	Stamp

	Position BlockPosition
	// Dimension names the world the position is in. Protocol 47 does not send
	// one, so it is empty there rather than guessed from the player's.
	Dimension string
	// Yaw and Pitch are the direction to face on respawning. Angled reports
	// whether a protocol sent them.
	Yaw, Pitch float32
	Angled     bool
}

func (WorldSpawnChanged) Name() Name     { return NameWorldSpawnChanged }
func (WorldSpawnChanged) Domain() Domain { return DomainWorld }

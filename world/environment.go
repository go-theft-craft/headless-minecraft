package world

import (
	"maps"
	"slices"

	"github.com/go-theft-craft/headless-minecraft/event"
)

// The environment is the world's scalars: the clock, the border, the weather,
// the difficulty, and the settings the server simulates under. They are not
// chunk data and they arrive from an entirely different packet set, which is
// why they are their own reducer rather than a pile of fields on the largest
// and most performance-sensitive domain in the milestone.
//
// Almost everything here is optional in one protocol or the other, so almost
// everything has a companion boolean. Zero is a legal difficulty, a legal view
// distance, and a legal time, and a caller cannot otherwise tell a server that
// said nothing from a server that said zero.

// maxGameRules bounds the game rules one connection may accumulate.
//
// Vanilla defines about fifty. A modded server defines its own, and the keys
// come from the peer, so the store is bounded like every other peer-filled
// store in this package.
const maxGameRules = 256

// maxClocks bounds the time-of-day clocks protocol 775 may declare. Vanilla
// sends one.
const maxClocks = 16

// Environment is the world's scalar state.
type Environment struct {
	age             int64
	timeOfDay       int64
	timeKnown       bool
	clocks          []event.Clock
	droppedClocks   int
	border          event.Border
	raining         bool
	rainLevel       float32
	thunderLevel    float32
	weatherKnown    bool
	difficulty      string
	locked          bool
	difficultyKnown bool

	viewDistance       int32
	simulationDistance int32
	viewChunkX         int32
	viewChunkZ         int32
	tickRate           float32
	frozen             bool
	simulationKnown    bool

	gameRules        map[string]string
	droppedGameRules int
}

// EnvironmentView is the environment half of a snapshot. Its maps and slices
// are owned copies.
type EnvironmentView struct {
	// Age is the world's total age in ticks, which both protocols send.
	Age int64
	// TimeOfDay is the position in the day cycle. TimeOfDayKnown reports
	// whether a protocol supplied one: protocol 47 sends it directly, and on
	// 775 it is read from the single clock a vanilla server declares.
	TimeOfDay      int64
	TimeOfDayKnown bool
	// Clocks is every clock protocol 775 sent, as sent. It is empty on 47,
	// which has no such concept.
	Clocks        []event.Clock
	DroppedClocks int

	Border event.Border

	Raining      bool
	RainLevel    float32
	ThunderLevel float32
	// WeatherKnown reports whether the server has said anything about weather.
	// A world nobody mentioned rain in is not a world that is known to be dry.
	WeatherKnown bool

	Difficulty string
	// Locked is protocol 775 only.
	Locked          bool
	DifficultyKnown bool

	ViewDistance       int32
	SimulationDistance int32
	ViewChunkX         int32
	ViewChunkZ         int32
	TickRate           float32
	Frozen             bool
	// SimulationKnown is false for the whole of a protocol 47 session, which
	// sends none of the five fields above.
	SimulationKnown bool

	GameRules        map[string]string
	DroppedGameRules int
}

func newEnvironment() *Environment {
	return &Environment{gameRules: make(map[string]string)}
}

func (e *Environment) view() EnvironmentView {
	return EnvironmentView{
		Age: e.age, TimeOfDay: e.timeOfDay, TimeOfDayKnown: e.timeKnown,
		Clocks: slices.Clone(e.clocks), DroppedClocks: e.droppedClocks,
		Border:       e.border,
		Raining:      e.raining,
		RainLevel:    e.rainLevel,
		ThunderLevel: e.thunderLevel,
		WeatherKnown: e.weatherKnown,
		Difficulty:   e.difficulty, Locked: e.locked, DifficultyKnown: e.difficultyKnown,
		ViewDistance: e.viewDistance, SimulationDistance: e.simulationDistance,
		ViewChunkX: e.viewChunkX, ViewChunkZ: e.viewChunkZ,
		TickRate: e.tickRate, Frozen: e.frozen, SimulationKnown: e.simulationKnown,
		GameRules: maps.Clone(e.gameRules), DroppedGameRules: e.droppedGameRules,
	}
}

// TimeChanged records the world clock. Protocol 47 supplies a time of day and
// 775 supplies clocks; each adapter passes what it has.
//
// The time of day is read from a single clock when 775 sends exactly one,
// because then there is no ambiguity about which clock the world runs on. With
// several, the caller reads Clocks and decides, since this client does not
// model the ID space and picking the first one would be a guess.
func (e *Environment) TimeChanged(
	c *event.Collector,
	age int64,
	timeOfDay int64,
	timeKnown bool,
	clocks []event.Clock,
) {
	e.age = age

	if len(clocks) > maxClocks {
		e.droppedClocks += len(clocks) - maxClocks
		clocks = clocks[:maxClocks]
	}
	if clocks != nil {
		e.clocks = slices.Clone(clocks)
	}
	if !timeKnown && len(clocks) == 1 {
		timeOfDay, timeKnown = clocks[0].TotalTicks, true
	}
	if timeKnown {
		e.timeOfDay, e.timeKnown = timeOfDay, true
	}

	event.Emit(c, event.WorldTimeChanged{
		Age: e.age, TimeOfDay: e.timeOfDay, TimeOfDayKnown: e.timeKnown,
		Clocks: slices.Clone(e.clocks),
	})
}

// The border mutators below are one per fact rather than one per packet.
// Protocol 47 sends six actions on one packet and 775 sends six packets, and
// both reduce to these five facts plus the initializing one — which is what
// makes the eleven produce one event and one snapshot.
//
// Each sets its own fields and leaves the rest, because a server that moves the
// centre has not said anything about the diameter.

// BorderInitialized records the packet that describes the whole border at once.
func (e *Environment) BorderInitialized(c *event.Collector, border event.Border) {
	border.Known = true
	e.border = border

	e.emitBorder(c)
}

// BorderCenter records the border moving.
func (e *Environment) BorderCenter(c *event.Collector, x, z float64) {
	e.border.X, e.border.Z = x, z

	e.emitBorder(c)
}

// BorderSize records an immediate resize, which is a move with no duration.
func (e *Environment) BorderSize(c *event.Collector, diameter float64) {
	e.border.OldDiameter, e.border.NewDiameter, e.border.Speed = diameter, diameter, 0

	e.emitBorder(c)
}

// BorderLerp records a resize over time.
func (e *Environment) BorderLerp(c *event.Collector, old, size float64, speed int64) {
	e.border.OldDiameter, e.border.NewDiameter, e.border.Speed = old, size, speed

	e.emitBorder(c)
}

// BorderWarningTime records the seconds of warning before the border arrives.
func (e *Environment) BorderWarningTime(c *event.Collector, seconds int32) {
	e.border.WarningTime = seconds

	e.emitBorder(c)
}

// BorderWarningBlocks records the distance at which the border warns.
func (e *Environment) BorderWarningBlocks(c *event.Collector, blocks int32) {
	e.border.WarningBlocks = blocks

	e.emitBorder(c)
}

func (e *Environment) emitBorder(c *event.Collector) {
	e.border.Known = true

	event.Emit(c, event.WorldBorderChanged{Border: e.border})
}

// WeatherChanged records rain starting or stopping.
func (e *Environment) WeatherChanged(c *event.Collector, raining bool) {
	e.raining, e.weatherKnown = raining, true
	if !raining {
		e.rainLevel = 0
	}

	e.emitWeather(c)
}

// RainLevelChanged records the rain fade value, which both protocols send
// separately from the start and stop of rain.
func (e *Environment) RainLevelChanged(c *event.Collector, level float32) {
	e.rainLevel, e.weatherKnown = level, true

	e.emitWeather(c)
}

// ThunderLevelChanged records the thunder fade value. Protocol 47 has no
// reason byte for it and never calls this.
func (e *Environment) ThunderLevelChanged(c *event.Collector, level float32) {
	e.thunderLevel, e.weatherKnown = level, true

	e.emitWeather(c)
}

func (e *Environment) emitWeather(c *event.Collector) {
	event.Emit(c, event.WorldWeatherChanged{
		Raining: e.raining, RainLevel: e.rainLevel, ThunderLevel: e.thunderLevel,
	})
}

// DifficultyChanged records the difficulty. The name is the adapter's, resolved
// from a number on protocol 47 and taken as sent on 775.
func (e *Environment) DifficultyChanged(c *event.Collector, difficulty string, locked bool) {
	e.difficulty, e.locked, e.difficultyKnown = difficulty, locked, true

	event.Emit(c, event.WorldDifficultyChanged{Difficulty: difficulty, Locked: locked})
}

// Explosion records an explosion. It changes no stored state: the blocks an
// explosion destroys arrive as block changes, and the knockback it applies
// arrives as the player's own position correction.
func (e *Environment) Explosion(c *event.Collector, explosion event.WorldExplosionOccurred) {
	event.Emit(c, explosion)
}

// WorldEvent records a discrete effect at a position. Like an explosion it
// stores nothing; a door that opened arrives as a block change too.
func (e *Environment) WorldEvent(
	c *event.Collector,
	effectID int32,
	pos BlockPos,
	data int32,
	global bool,
) {
	event.Emit(c, event.WorldEventOccurred{
		EffectID: effectID,
		Position: event.BlockPosition{X: pos.X, Y: pos.Y, Z: pos.Z},
		Data:     data, Global: global,
	})
}

// The simulation mutators follow the border's rule: one per fact, each setting
// its own fields and publishing the resulting settings. None of them fires on
// protocol 47, which sends none of these packets.

// ViewDistanceChanged records how far the server streams chunks.
func (e *Environment) ViewDistanceChanged(c *event.Collector, chunks int32) {
	e.viewDistance = chunks

	e.emitSimulation(c, nil)
}

// SimulationDistanceChanged records how far the server ticks entities.
func (e *Environment) SimulationDistanceChanged(c *event.Collector, chunks int32) {
	e.simulationDistance = chunks

	e.emitSimulation(c, nil)
}

// ViewCenterChanged records the chunk the server streams around, which follows
// the player and is what the view distance is measured from.
func (e *Environment) ViewCenterChanged(c *event.Collector, chunkX, chunkZ int32) {
	e.viewChunkX, e.viewChunkZ = chunkX, chunkZ

	e.emitSimulation(c, nil)
}

// TickingStateChanged records the server's tick rate and whether it is frozen.
func (e *Environment) TickingStateChanged(c *event.Collector, rate float32, frozen bool) {
	e.tickRate, e.frozen = rate, frozen

	e.emitSimulation(c, nil)
}

// GameRulesChanged merges game rules by name, bounded like every other
// peer-filled store here. The values are kept as sent: a rule this client has
// no name for is exactly the rule a modded server defines.
func (e *Environment) GameRulesChanged(c *event.Collector, rules map[string]string) {
	if len(rules) == 0 {
		return
	}

	keys := make([]string, 0, len(rules))
	for name, value := range rules {
		if _, existing := e.gameRules[name]; !existing && len(e.gameRules) >= maxGameRules {
			e.droppedGameRules++

			continue
		}
		e.gameRules[name] = value
		keys = append(keys, name)
	}
	if len(keys) == 0 {
		return
	}
	slices.Sort(keys)
	e.simulationKnown = true

	e.emitSimulation(c, keys)
}

func (e *Environment) emitSimulation(c *event.Collector, ruleKeys []string) {
	e.simulationKnown = true

	event.Emit(c, event.WorldSimulationSettingsChanged{
		ViewDistance: e.viewDistance, SimulationDistance: e.simulationDistance,
		ViewChunkX: e.viewChunkX, ViewChunkZ: e.viewChunkZ,
		TickRate: e.tickRate, Frozen: e.frozen, RuleKeys: ruleKeys,
	})
}

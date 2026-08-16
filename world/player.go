package world

import (
	"maps"

	"github.com/go-theft-craft/headless-minecraft/event"
)

// Player is the local player's observed state.
//
// It holds what the server said, never what a movement strategy intends: every
// position here is a server correction, and M9 owns the other direction.
//
// The mutators below are the world's contract with a version adapter. The
// adapter decodes one protocol's packets and calls these; the state, the
// arithmetic, and the events are shared, because those are the parts two
// protocols agree on.
type Player struct {
	entityID  int32
	known     bool
	dimension string
	gameMode  uint8

	x, y, z    float64
	yaw, pitch float32
	placed     bool

	health     float32
	food       int32
	saturation float32

	experienceBar   float32
	level           int32
	totalExperience int32

	abilityFlags int8
	flyingSpeed  float32
	walkingSpeed float32

	heldSlot int32

	effects   map[int32]Effect
	cooldowns map[string]int32
}

// Effect is one status effect the server applied to the local player.
type Effect struct {
	ID        int32
	Amplifier int32
	Duration  int32
}

// PlayerView is the player's half of a snapshot. Its maps are owned copies.
type PlayerView struct {
	// Known reports whether the play login has arrived. Until it has, every
	// other field is zero rather than unknown, and this is how a caller tells
	// those apart.
	Known     bool
	EntityID  int32
	Dimension string
	GameMode  uint8

	X, Y, Z    float64
	Yaw, Pitch float32
	// Placed reports whether the server has ever sent a position. A player
	// at the origin and a player the server has not placed are different.
	Placed bool

	Health     float32
	Food       int32
	Saturation float32

	ExperienceBar   float32
	Level           int32
	TotalExperience int32

	AbilityFlags int8
	FlyingSpeed  float32
	WalkingSpeed float32

	HeldSlot int32

	Effects   map[int32]Effect
	Cooldowns map[string]int32
}

func newPlayer() *Player {
	return &Player{
		effects:   make(map[int32]Effect),
		cooldowns: make(map[string]int32),
	}
}

// view returns the player's snapshot half. It runs under the world's lock.
func (p *Player) view() PlayerView {
	return PlayerView{
		Known: p.known, EntityID: p.entityID, Dimension: p.dimension, GameMode: p.gameMode,
		X: p.x, Y: p.y, Z: p.z, Yaw: p.yaw, Pitch: p.pitch, Placed: p.placed,
		Health: p.health, Food: p.food, Saturation: p.saturation,
		ExperienceBar: p.experienceBar, Level: p.level, TotalExperience: p.totalExperience,
		AbilityFlags: p.abilityFlags, FlyingSpeed: p.flyingSpeed, WalkingSpeed: p.walkingSpeed,
		HeldSlot:  p.heldSlot,
		Effects:   maps.Clone(p.effects),
		Cooldowns: maps.Clone(p.cooldowns),
	}
}

// EntityID reports the local player's entity ID and whether the server has
// said what it is. Every reducer after the player's uses this to tell the
// local player apart from every other entity.
func (p *Player) EntityID() (int32, bool) { return p.entityID, p.known }

// Login records the play login: the entity the server will call this player,
// the world it is in, and the mode it plays in.
func (p *Player) Login(c *event.Collector, entityID int32, dimension string, gameMode uint8) {
	p.entityID, p.known = entityID, true
	p.dimension, p.gameMode = dimension, gameMode

	event.Emit(c, event.PlayerSpawned{
		EntityID: entityID, Dimension: dimension, GameMode: gameMode,
	})
}

// Relative says which components of a position packet are offsets from the
// player's current position rather than absolute coordinates.
//
// Both protocols send this, in different shapes: protocol 47 packs it into a
// flags byte and 775 into a struct of booleans. Resolving it needs a previous
// position, which is why it belongs here and not in the readiness rule.
type Relative struct {
	X, Y, Z, Yaw, Pitch bool
}

// Any reports whether any component is relative.
func (r Relative) Any() bool { return r.X || r.Y || r.Z || r.Yaw || r.Pitch }

// Move applies a position the server sent, resolving each relative component
// against the position this player already had.
func (p *Player) Move(
	c *event.Collector,
	x, y, z float64,
	yaw, pitch float32,
	relative Relative,
) {
	if relative.X {
		x += p.x
	}
	if relative.Y {
		y += p.y
	}
	if relative.Z {
		z += p.z
	}
	if relative.Yaw {
		yaw += p.yaw
	}
	if relative.Pitch {
		pitch += p.pitch
	}

	p.x, p.y, p.z, p.yaw, p.pitch = x, y, z, yaw, pitch
	p.placed = true

	event.Emit(c, event.PlayerMoved{
		X: x, Y: y, Z: z, Yaw: yaw, Pitch: pitch, Relative: relative.Any(),
	})
}

// Look applies a rotation with no position, which protocol 775 can send on its
// own.
func (p *Player) Look(c *event.Collector, yaw, pitch float32, relative Relative) {
	p.Move(c, p.x, p.y, p.z, yaw, pitch, Relative{Yaw: relative.Yaw, Pitch: relative.Pitch})
}

// Health records health, food, and saturation.
func (p *Player) Health(c *event.Collector, health float32, food int32, saturation float32) {
	p.health, p.food, p.saturation = health, food, saturation

	event.Emit(c, event.PlayerHealthChanged{Health: health, Food: food, Saturation: saturation})
}

// Experience records the experience bar, level, and total.
func (p *Player) Experience(c *event.Collector, bar float32, level, total int32) {
	p.experienceBar, p.level, p.totalExperience = bar, level, total

	event.Emit(c, event.PlayerExperienceChanged{Bar: bar, Level: level, Total: total})
}

// Abilities records the ability flags and speeds the server granted.
func (p *Player) Abilities(c *event.Collector, flags int8, flying, walking float32) {
	p.abilityFlags, p.flyingSpeed, p.walkingSpeed = flags, flying, walking

	event.Emit(c, event.PlayerAbilitiesChanged{
		Flags: flags, FlyingSpeed: flying, WalkingSpeed: walking,
	})
}

// GameMode records a game-mode change.
func (p *Player) GameMode(c *event.Collector, mode uint8) {
	p.gameMode = mode

	event.Emit(c, event.PlayerGameModeChanged{GameMode: mode})
}

// Respawn records a respawn, which can change dimension and game mode at once
// and leaves the player unplaced until the server sends a position.
func (p *Player) Respawn(c *event.Collector, dimension string, gameMode uint8) {
	p.dimension, p.gameMode = dimension, gameMode
	p.placed = false
	clear(p.effects)

	event.Emit(c, event.PlayerRespawned{Dimension: dimension, GameMode: gameMode})
}

// HeldSlot records the server moving the held hotbar slot.
func (p *Player) HeldSlot(c *event.Collector, slot int32) {
	p.heldSlot = slot

	event.Emit(c, event.PlayerHeldSlotChanged{Slot: slot})
}

// EffectApplied records a status effect starting or changing.
func (p *Player) EffectApplied(c *event.Collector, id, amplifier, duration int32) {
	p.effects[id] = Effect{ID: id, Amplifier: amplifier, Duration: duration}

	event.Emit(c, event.PlayerEffectsChanged{
		EffectID: id, Amplifier: amplifier, Duration: duration,
	})
}

// EffectRemoved records a status effect ending. Removing one the player never
// had is not an error: the server may have applied it before this client
// connected.
func (p *Player) EffectRemoved(c *event.Collector, id int32) {
	delete(p.effects, id)

	event.Emit(c, event.PlayerEffectsChanged{EffectID: id, Removed: true})
}

// Cooldown records an item cooldown starting, or ending when ticks is zero.
func (p *Player) Cooldown(c *event.Collector, group string, ticks int32) {
	if ticks <= 0 {
		delete(p.cooldowns, group)
	} else {
		p.cooldowns[group] = ticks
	}

	event.Emit(c, event.PlayerCooldownChanged{Group: group, Ticks: ticks})
}

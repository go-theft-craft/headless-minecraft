package event

// The player domain describes the local player only. Everything the server
// says about somebody else is an entity, including another player.

// PlayerSpawned reports the local player entering a world. It is the first
// thing a play session learns, and it names the entity the server will use
// for the local player from then on.
type PlayerSpawned struct {
	Stamp

	EntityID  int32
	Dimension string
	GameMode  uint8
}

func (PlayerSpawned) Name() Name     { return NamePlayerSpawned }
func (PlayerSpawned) Domain() Domain { return DomainPlayer }

// PlayerMoved reports a position the server placed the player at.
//
// It is the server's correction, not this client's movement: a movement
// strategy is M9, and until then every position in this domain came from the
// server.
type PlayerMoved struct {
	Stamp

	X, Y, Z    float64
	Yaw, Pitch float32
	// Relative reports that the server sent an offset rather than an
	// absolute position. The coordinates above are already resolved against
	// the previous position; this says how they arrived.
	Relative bool
}

func (PlayerMoved) Name() Name     { return NamePlayerMoved }
func (PlayerMoved) Domain() Domain { return DomainPlayer }

// PlayerHealthChanged reports health, food, or saturation changing.
type PlayerHealthChanged struct {
	Stamp

	Health     float32
	Food       int32
	Saturation float32
}

func (PlayerHealthChanged) Name() Name     { return NamePlayerHealthChanged }
func (PlayerHealthChanged) Domain() Domain { return DomainPlayer }

// PlayerExperienceChanged reports the experience bar, level, or total moving.
type PlayerExperienceChanged struct {
	Stamp

	Bar   float32
	Level int32
	Total int32
}

func (PlayerExperienceChanged) Name() Name     { return NamePlayerExperienceChanged }
func (PlayerExperienceChanged) Domain() Domain { return DomainPlayer }

// PlayerAbilitiesChanged reports flight, invulnerability, and the speeds the
// server grants. Flags is the server's own bitfield, kept as sent: a modded
// server may define bits this client has no name for.
type PlayerAbilitiesChanged struct {
	Stamp

	Flags        int8
	FlyingSpeed  float32
	WalkingSpeed float32
}

func (PlayerAbilitiesChanged) Name() Name     { return NamePlayerAbilitiesChanged }
func (PlayerAbilitiesChanged) Domain() Domain { return DomainPlayer }

// PlayerGameModeChanged reports a game-mode change.
type PlayerGameModeChanged struct {
	Stamp

	GameMode uint8
}

func (PlayerGameModeChanged) Name() Name     { return NamePlayerGameModeChanged }
func (PlayerGameModeChanged) Domain() Domain { return DomainPlayer }

// PlayerRespawned reports the player respawning, which resets far more than
// position: a respawn can change dimension and game mode at once.
type PlayerRespawned struct {
	Stamp

	Dimension string
	GameMode  uint8
}

func (PlayerRespawned) Name() Name     { return NamePlayerRespawned }
func (PlayerRespawned) Domain() Domain { return DomainPlayer }

// PlayerHeldSlotChanged reports the server moving the held hotbar slot.
type PlayerHeldSlotChanged struct {
	Stamp

	Slot int32
}

func (PlayerHeldSlotChanged) Name() Name     { return NamePlayerHeldSlotChanged }
func (PlayerHeldSlotChanged) Domain() Domain { return DomainPlayer }

// PlayerEffectsChanged reports one status effect arriving, changing, or
// ending. Removed says which.
type PlayerEffectsChanged struct {
	Stamp

	EffectID  int32
	Amplifier int32
	Duration  int32
	Removed   bool
}

func (PlayerEffectsChanged) Name() Name     { return NamePlayerEffectsChanged }
func (PlayerEffectsChanged) Domain() Domain { return DomainPlayer }

// PlayerCooldownChanged reports an item cooldown starting or ending.
//
// Group is the cooldown's identifier. Protocol 47 has no cooldown packet at
// all, so this only ever fires on 775.
type PlayerCooldownChanged struct {
	Stamp

	Group string
	Ticks int32
}

func (PlayerCooldownChanged) Name() Name     { return NamePlayerCooldownChanged }
func (PlayerCooldownChanged) Domain() Domain { return DomainPlayer }

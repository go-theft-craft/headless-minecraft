package event

import "slices"

// The taxonomy fixes every name now so that later milestones extend this
// surface rather than reshaping it. Only the session events have structs in
// this milestone; the rest are names, which gives a consumer the contract
// without leaving unused structs behind.
//
// Every name below is derived from a packet that exists in the pinned
// protocol 775 or protocol 47 schema.

// Session events.
const (
	SessionConnecting            Name = "session.connecting"
	SessionAuthenticated         Name = "session.authenticated"
	SessionStateChanged          Name = "session.state_changed"
	SessionReady                 Name = "session.ready"
	SessionDisconnected          Name = "session.disconnected"
	SessionClosed                Name = "session.closed"
	SessionKeepAlivePonged       Name = "session.keepalive_ponged"
	SessionTransferRequested     Name = "session.transfer_requested"
	SessionResourcePackOffered   Name = "session.resource_pack_offered"
	SessionResourcePackRevoked   Name = "session.resource_pack_revoked"
	SessionServerMetadataChanged Name = "session.server_metadata_changed"
	SessionCookieRequested       Name = "session.cookie_requested"
	SessionCookieStored          Name = "session.cookie_stored"
	SessionCustomPayloadReceived Name = "session.custom_payload_received"
	// SessionPacketReceived and SessionPacketSent report DomainRaw rather than
	// DomainSession. Raw delivery is a selector, not a taxonomy entry: the
	// names exist so a log line can identify them, and a subscriber opts into
	// them separately.
	SessionPacketReceived Name = "session.packet_received"
	SessionPacketSent     Name = "session.packet_sent"
)

// Player events.
const (
	PlayerSpawned           Name = "player.spawned"
	PlayerMoved             Name = "player.moved"
	PlayerHealthChanged     Name = "player.health_changed"
	PlayerExperienceChanged Name = "player.experience_changed"
	PlayerAbilitiesChanged  Name = "player.abilities_changed"
	PlayerGameModeChanged   Name = "player.game_mode_changed"
	PlayerRespawned         Name = "player.respawned"
	PlayerHeldSlotChanged   Name = "player.held_slot_changed"
	PlayerEffectsChanged    Name = "player.effects_changed"
	PlayerCooldownChanged   Name = "player.cooldown_changed"
)

// World events.
const (
	WorldChunkLoaded               Name = "world.chunk_loaded"
	WorldChunkUnloaded             Name = "world.chunk_unloaded"
	WorldBlocksChanged             Name = "world.blocks_changed"
	WorldBlockEntityChanged        Name = "world.block_entity_changed"
	WorldLightChanged              Name = "world.light_changed"
	WorldTimeChanged               Name = "world.time_changed"
	WorldBorderChanged             Name = "world.border_changed"
	WorldWeatherChanged            Name = "world.weather_changed"
	WorldDifficultyChanged         Name = "world.difficulty_changed"
	WorldExplosionOccurred         Name = "world.explosion_occurred"
	WorldEventOccurred             Name = "world.event_occurred"
	WorldSimulationSettingsChanged Name = "world.simulation_settings_changed"
)

// Entity events.
const (
	EntitySpawned           Name = "entity.spawned"
	EntityRemoved           Name = "entity.removed"
	EntityMoved             Name = "entity.moved"
	EntityMetadataChanged   Name = "entity.metadata_changed"
	EntityEquipmentChanged  Name = "entity.equipment_changed"
	EntityAttributesChanged Name = "entity.attributes_changed"
	EntityEffectsChanged    Name = "entity.effects_changed"
	EntityVelocityChanged   Name = "entity.velocity_changed"
	EntityPassengersChanged Name = "entity.passengers_changed"
	EntityDamaged           Name = "entity.damaged"
	EntityAnimated          Name = "entity.animated"
	EntityItemCollected     Name = "entity.item_collected"
)

// Container events.
const (
	ContainerOpened         Name = "container.opened"
	ContainerClosed         Name = "container.closed"
	ContainerSlotsChanged   Name = "container.slots_changed"
	ContainerCursorChanged  Name = "container.cursor_changed"
	ContainerRecipesChanged Name = "container.recipes_changed"
	ContainerTradesChanged  Name = "container.trades_changed"
	ContainerCraftResponse  Name = "container.craft_response"
)

// Registry events.
const (
	RegistryDataReceived      Name = "registry.data_received"
	RegistryTagsReceived      Name = "registry.tags_received"
	RegistryCommandsReceived  Name = "registry.commands_received"
	RegistryPlayerListChanged Name = "registry.player_list_changed"
)

// Chat and UI events.
const (
	ChatReceived            Name = "chat.received"
	ChatRemoved             Name = "chat.removed"
	ChatTitleChanged        Name = "chat.title_changed"
	ChatActionBarChanged    Name = "chat.action_bar_changed"
	ChatBossBarChanged      Name = "chat.boss_bar_changed"
	ChatScoreboardChanged   Name = "chat.scoreboard_changed"
	ChatTeamsChanged        Name = "chat.teams_changed"
	ChatAdvancementsChanged Name = "chat.advancements_changed"
	ChatSoundPlayed         Name = "chat.sound_played"
	ChatStatisticsReceived  Name = "chat.statistics_received"
	ChatDialogShown         Name = "chat.dialog_shown"
	ChatTabCompleted        Name = "chat.tab_completed"
)

// domains maps each name to its domain. It is the single source of truth:
// Domain reads it, and AllNames enumerates it.
//
// The two raw names are deliberately absent. Their structs report DomainRaw,
// which is what a subscriber's selector matches; Name.Domain returns zero
// for them, which is what keeps them out of the named taxonomy.
var domains = map[Name]Domain{
	SessionConnecting: DomainSession, SessionAuthenticated: DomainSession,
	SessionStateChanged: DomainSession, SessionReady: DomainSession,
	SessionDisconnected: DomainSession, SessionClosed: DomainSession,
	SessionKeepAlivePonged: DomainSession, SessionTransferRequested: DomainSession,
	SessionResourcePackOffered: DomainSession, SessionResourcePackRevoked: DomainSession,
	SessionServerMetadataChanged: DomainSession, SessionCookieRequested: DomainSession,
	SessionCookieStored: DomainSession, SessionCustomPayloadReceived: DomainSession,

	PlayerSpawned: DomainPlayer, PlayerMoved: DomainPlayer,
	PlayerHealthChanged: DomainPlayer, PlayerExperienceChanged: DomainPlayer,
	PlayerAbilitiesChanged: DomainPlayer, PlayerGameModeChanged: DomainPlayer,
	PlayerRespawned: DomainPlayer, PlayerHeldSlotChanged: DomainPlayer,
	PlayerEffectsChanged: DomainPlayer, PlayerCooldownChanged: DomainPlayer,

	WorldChunkLoaded: DomainWorld, WorldChunkUnloaded: DomainWorld,
	WorldBlocksChanged: DomainWorld, WorldBlockEntityChanged: DomainWorld,
	WorldLightChanged: DomainWorld, WorldTimeChanged: DomainWorld,
	WorldBorderChanged: DomainWorld, WorldWeatherChanged: DomainWorld,
	WorldDifficultyChanged: DomainWorld, WorldExplosionOccurred: DomainWorld,
	WorldEventOccurred: DomainWorld, WorldSimulationSettingsChanged: DomainWorld,

	EntitySpawned: DomainEntities, EntityRemoved: DomainEntities,
	EntityMoved: DomainEntities, EntityMetadataChanged: DomainEntities,
	EntityEquipmentChanged: DomainEntities, EntityAttributesChanged: DomainEntities,
	EntityEffectsChanged: DomainEntities, EntityVelocityChanged: DomainEntities,
	EntityPassengersChanged: DomainEntities, EntityDamaged: DomainEntities,
	EntityAnimated: DomainEntities, EntityItemCollected: DomainEntities,

	ContainerOpened: DomainContainers, ContainerClosed: DomainContainers,
	ContainerSlotsChanged: DomainContainers, ContainerCursorChanged: DomainContainers,
	ContainerRecipesChanged: DomainContainers, ContainerTradesChanged: DomainContainers,
	ContainerCraftResponse: DomainContainers,

	RegistryDataReceived: DomainRegistry, RegistryTagsReceived: DomainRegistry,
	RegistryCommandsReceived: DomainRegistry, RegistryPlayerListChanged: DomainRegistry,

	ChatReceived: DomainChat, ChatRemoved: DomainChat,
	ChatTitleChanged: DomainChat, ChatActionBarChanged: DomainChat,
	ChatBossBarChanged: DomainChat, ChatScoreboardChanged: DomainChat,
	ChatTeamsChanged: DomainChat, ChatAdvancementsChanged: DomainChat,
	ChatSoundPlayed: DomainChat, ChatStatisticsReceived: DomainChat,
	ChatDialogShown: DomainChat, ChatTabCompleted: DomainChat,
}

// Domain reports which domain an event name belongs to, or zero when the name
// is not part of the taxonomy.
func (n Name) Domain() Domain { return domains[n] }

// AllNames returns every declared event name in sorted order.
func AllNames() []Name {
	names := make([]Name, 0, len(domains))
	for name := range domains {
		names = append(names, name)
	}
	slices.Sort(names)

	return names
}

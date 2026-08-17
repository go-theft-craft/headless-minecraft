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
	NameSessionConnecting            Name = "session.connecting"
	NameSessionAuthenticated         Name = "session.authenticated"
	NameSessionStateChanged          Name = "session.state_changed"
	NameSessionReady                 Name = "session.ready"
	NameSessionDisconnected          Name = "session.disconnected"
	NameSessionClosed                Name = "session.closed"
	NameSessionKeepAlivePonged       Name = "session.keepalive_ponged"
	NameSessionTransferRequested     Name = "session.transfer_requested"
	NameSessionResourcePackOffered   Name = "session.resource_pack_offered"
	NameSessionResourcePackRevoked   Name = "session.resource_pack_revoked"
	NameSessionServerMetadataChanged Name = "session.server_metadata_changed"
	NameSessionCookieRequested       Name = "session.cookie_requested"
	NameSessionCookieStored          Name = "session.cookie_stored"
	NameSessionCustomPayloadReceived Name = "session.custom_payload_received"
	// NameSessionObservationMissing is the one name here that no packet
	// carries: it reports a packet that never arrived. See ObservationMissing.
	NameSessionObservationMissing Name = "session.observation_missing"
	// NameSessionPacketReceived and NameSessionPacketSent report DomainRaw rather than
	// DomainSession. Raw delivery is a selector, not a taxonomy entry: the
	// names exist so a log line can identify them, and a subscriber opts into
	// them separately.
	NameSessionPacketReceived Name = "session.packet_received"
	NameSessionPacketSent     Name = "session.packet_sent"
)

// Player events.
const (
	NamePlayerSpawned           Name = "player.spawned"
	NamePlayerMoved             Name = "player.moved"
	NamePlayerHealthChanged     Name = "player.health_changed"
	NamePlayerDamaged           Name = "player.damaged"
	NamePlayerDied              Name = "player.died"
	NamePlayerExperienceChanged Name = "player.experience_changed"
	NamePlayerAbilitiesChanged  Name = "player.abilities_changed"
	NamePlayerGameModeChanged   Name = "player.game_mode_changed"
	NamePlayerRespawned         Name = "player.respawned"
	NamePlayerHeldSlotChanged   Name = "player.held_slot_changed"
	NamePlayerEffectsChanged    Name = "player.effects_changed"
	NamePlayerCooldownChanged   Name = "player.cooldown_changed"
)

// World events.
const (
	NameWorldChunkLoaded               Name = "world.chunk_loaded"
	NameWorldChunkUnloaded             Name = "world.chunk_unloaded"
	NameWorldBlocksChanged             Name = "world.blocks_changed"
	NameWorldBlockEntityChanged        Name = "world.block_entity_changed"
	NameWorldLightChanged              Name = "world.light_changed"
	NameWorldTimeChanged               Name = "world.time_changed"
	NameWorldBorderChanged             Name = "world.border_changed"
	NameWorldWeatherChanged            Name = "world.weather_changed"
	NameWorldDifficultyChanged         Name = "world.difficulty_changed"
	NameWorldExplosionOccurred         Name = "world.explosion_occurred"
	NameWorldEventOccurred             Name = "world.event_occurred"
	NameWorldSimulationSettingsChanged Name = "world.simulation_settings_changed"
	NameWorldSpawnChanged              Name = "world.spawn_changed"
)

// Entity events.
const (
	NameEntitySpawned           Name = "entity.spawned"
	NameEntityRemoved           Name = "entity.removed"
	NameEntityMoved             Name = "entity.moved"
	NameEntityMetadataChanged   Name = "entity.metadata_changed"
	NameEntityEquipmentChanged  Name = "entity.equipment_changed"
	NameEntityAttributesChanged Name = "entity.attributes_changed"
	NameEntityEffectsChanged    Name = "entity.effects_changed"
	NameEntityVelocityChanged   Name = "entity.velocity_changed"
	NameEntityPassengersChanged Name = "entity.passengers_changed"
	NameEntityDamaged           Name = "entity.damaged"
	NameEntityDied              Name = "entity.died"
	NameEntityAnimated          Name = "entity.animated"
	NameEntityItemCollected     Name = "entity.item_collected"
)

// Container events.
const (
	NameContainerOpened         Name = "container.opened"
	NameContainerClosed         Name = "container.closed"
	NameContainerSlotsChanged   Name = "container.slots_changed"
	NameContainerCursorChanged  Name = "container.cursor_changed"
	NameContainerRecipesChanged Name = "container.recipes_changed"
	NameContainerTradesChanged  Name = "container.trades_changed"
	NameContainerCraftResponse  Name = "container.craft_response"
)

// Registry events.
const (
	NameRegistryDataReceived      Name = "registry.data_received"
	NameRegistryTagsReceived      Name = "registry.tags_received"
	NameRegistryCommandsReceived  Name = "registry.commands_received"
	NameRegistryPlayerListChanged Name = "registry.player_list_changed"
)

// Chat and UI events.
const (
	NameChatReceived            Name = "chat.received"
	NameChatRemoved             Name = "chat.removed"
	NameChatTitleChanged        Name = "chat.title_changed"
	NameChatActionBarChanged    Name = "chat.action_bar_changed"
	NameChatBossBarChanged      Name = "chat.boss_bar_changed"
	NameChatScoreboardChanged   Name = "chat.scoreboard_changed"
	NameChatTeamsChanged        Name = "chat.teams_changed"
	NameChatAdvancementsChanged Name = "chat.advancements_changed"
	NameChatSoundPlayed         Name = "chat.sound_played"
	NameChatStatisticsReceived  Name = "chat.statistics_received"
	NameChatDialogShown         Name = "chat.dialog_shown"
	NameChatTabCompleted        Name = "chat.tab_completed"
)

// domains maps each name to its domain. It is the single source of truth:
// Domain reads it, and AllNames enumerates it.
//
// The two raw names are deliberately absent. Their structs report DomainRaw,
// which is what a subscriber's selector matches; Name.Domain returns zero
// for them, which is what keeps them out of the named taxonomy.
var domains = map[Name]Domain{
	NameSessionConnecting: DomainSession, NameSessionAuthenticated: DomainSession,
	NameSessionStateChanged: DomainSession, NameSessionReady: DomainSession,
	NameSessionDisconnected: DomainSession, NameSessionClosed: DomainSession,
	NameSessionKeepAlivePonged: DomainSession, NameSessionTransferRequested: DomainSession,
	NameSessionResourcePackOffered: DomainSession, NameSessionResourcePackRevoked: DomainSession,
	NameSessionServerMetadataChanged: DomainSession, NameSessionCookieRequested: DomainSession,
	NameSessionCookieStored: DomainSession, NameSessionCustomPayloadReceived: DomainSession,
	NameSessionObservationMissing: DomainSession,

	NamePlayerSpawned: DomainPlayer, NamePlayerMoved: DomainPlayer,
	NamePlayerHealthChanged: DomainPlayer, NamePlayerDamaged: DomainPlayer,
	NamePlayerDied: DomainPlayer, NamePlayerExperienceChanged: DomainPlayer,
	NamePlayerAbilitiesChanged: DomainPlayer, NamePlayerGameModeChanged: DomainPlayer,
	NamePlayerRespawned: DomainPlayer, NamePlayerHeldSlotChanged: DomainPlayer,
	NamePlayerEffectsChanged: DomainPlayer, NamePlayerCooldownChanged: DomainPlayer,

	NameWorldChunkLoaded: DomainWorld, NameWorldChunkUnloaded: DomainWorld,
	NameWorldBlocksChanged: DomainWorld, NameWorldBlockEntityChanged: DomainWorld,
	NameWorldLightChanged: DomainWorld, NameWorldTimeChanged: DomainWorld,
	NameWorldBorderChanged: DomainWorld, NameWorldWeatherChanged: DomainWorld,
	NameWorldDifficultyChanged: DomainWorld, NameWorldExplosionOccurred: DomainWorld,
	NameWorldEventOccurred: DomainWorld, NameWorldSimulationSettingsChanged: DomainWorld,
	NameWorldSpawnChanged: DomainWorld,

	NameEntitySpawned: DomainEntities, NameEntityRemoved: DomainEntities,
	NameEntityMoved: DomainEntities, NameEntityMetadataChanged: DomainEntities,
	NameEntityEquipmentChanged: DomainEntities, NameEntityAttributesChanged: DomainEntities,
	NameEntityEffectsChanged: DomainEntities, NameEntityVelocityChanged: DomainEntities,
	NameEntityPassengersChanged: DomainEntities, NameEntityDamaged: DomainEntities,
	NameEntityDied: DomainEntities, NameEntityAnimated: DomainEntities,
	NameEntityItemCollected: DomainEntities,

	NameContainerOpened: DomainContainers, NameContainerClosed: DomainContainers,
	NameContainerSlotsChanged: DomainContainers, NameContainerCursorChanged: DomainContainers,
	NameContainerRecipesChanged: DomainContainers, NameContainerTradesChanged: DomainContainers,
	NameContainerCraftResponse: DomainContainers,

	NameRegistryDataReceived: DomainRegistry, NameRegistryTagsReceived: DomainRegistry,
	NameRegistryCommandsReceived: DomainRegistry, NameRegistryPlayerListChanged: DomainRegistry,

	NameChatReceived: DomainChat, NameChatRemoved: DomainChat,
	NameChatTitleChanged: DomainChat, NameChatActionBarChanged: DomainChat,
	NameChatBossBarChanged: DomainChat, NameChatScoreboardChanged: DomainChat,
	NameChatTeamsChanged: DomainChat, NameChatAdvancementsChanged: DomainChat,
	NameChatSoundPlayed: DomainChat, NameChatStatisticsReceived: DomainChat,
	NameChatDialogShown: DomainChat, NameChatTabCompleted: DomainChat,
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

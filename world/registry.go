package world

import (
	"maps"
	"slices"

	"github.com/go-theft-craft/headless-minecraft/event"
)

// The registry domain holds the vocabulary the server defined for this
// connection: its registries, its tags, its command tree, and its player list.
//
// A session registry overrides the generated one for this connection, which is
// why this domain exists at all. The generated data stays reachable for
// lookups that do not depend on server configuration, and nothing here merges
// the two — a caller that resolves an ID decides which source it means.

const (
	// maxRegistries bounds the registries one connection may define. Vanilla
	// 26.1 sends about twenty; a modded server sends more, and the keys come
	// from the peer.
	maxRegistries = 512
	// maxTagTypes bounds the tag types.
	maxTagTypes = 512
	// maxListedPlayers bounds the server's player list. A large public server
	// lists thousands, and a list that grows without limit is the memory bug a
	// week-long session hits.
	maxListedPlayers = 10000
)

// Registry is one registry the server defined.
type Registry struct {
	// Key is the namespaced registry key, kept as sent.
	Key string
	// Entries names every entry in declaration order, because a registry's
	// order is its numeric ID space: entry 0 is the first.
	Entries []string
	// State is the protocol state the definition arrived in.
	State string
}

// ListedPlayer is one player on the server's list. It is not an entity: a
// listed player may be nowhere near this client.
type ListedPlayer struct {
	UUID string
	Name string
	// GameMode and Latency are the server's own numbers. Protocol 47 sends
	// both on every add; 775 sends each only when its action bit is set.
	GameMode int32
	Latency  int32
	Listed   bool
}

// Registries is the server's vocabulary for this connection.
type Registries struct {
	defined  map[string]Registry
	tagTypes map[string]int
	// commandNodes is the size of the last command tree, and commandsKnown
	// says whether one arrived: a tree of zero nodes and no tree are
	// different.
	commandNodes  int
	commandsKnown bool

	players map[string]ListedPlayer

	dropped        int
	droppedPlayers int
}

// RegistriesView is the registry half of a snapshot. Its maps are owned copies.
type RegistriesView struct {
	// Defined is every registry the server sent, by key. It is empty for a
	// whole protocol 47 session, which has no registry-data packet, and
	// SessionRegistries reports which of the two that is.
	Defined map[string]Registry
	// SessionRegistries reports whether the server defined any registry at
	// all. False means the connection runs entirely on generated data.
	SessionRegistries bool

	TagTypes map[string]int
	TagsSent bool

	CommandNodes  int
	CommandsKnown bool

	Players map[string]ListedPlayer

	Dropped        int
	DroppedPlayers int
}

// Get returns one registry, or false when the server did not define it.
func (v RegistriesView) Get(key string) (Registry, bool) {
	r, ok := v.Defined[key]

	return r, ok
}

func newRegistries() *Registries {
	return &Registries{
		defined:  make(map[string]Registry),
		tagTypes: make(map[string]int),
		players:  make(map[string]ListedPlayer),
	}
}

func (s *Registries) view() RegistriesView {
	defined := make(map[string]Registry, len(s.defined))
	for key, registry := range s.defined {
		registry.Entries = slices.Clone(registry.Entries)
		defined[key] = registry
	}

	return RegistriesView{
		Defined:           defined,
		SessionRegistries: len(s.defined) > 0,
		TagTypes:          maps.Clone(s.tagTypes),
		TagsSent:          len(s.tagTypes) > 0,
		CommandNodes:      s.commandNodes,
		CommandsKnown:     s.commandsKnown,
		Players:           maps.Clone(s.players),
		Dropped:           s.dropped,
		DroppedPlayers:    s.droppedPlayers,
	}
}

// DataReceived records one registry the server defined, replacing any earlier
// definition of the same key: a server that resends a registry has changed it.
func (s *Registries) DataReceived(c *event.Collector, key string, entries []string, state string) {
	if _, existing := s.defined[key]; !existing && len(s.defined) >= maxRegistries {
		s.dropped++

		return
	}
	s.defined[key] = Registry{Key: key, Entries: slices.Clone(entries), State: state}

	event.Emit(c, event.RegistryDataReceived{
		Registry: key, Entries: len(entries), State: state,
	})
}

// TagsReceived records the tag types the server sent and how many tags each
// carried.
func (s *Registries) TagsReceived(c *event.Collector, counts map[string]int) {
	if len(counts) == 0 {
		return
	}

	types := make([]string, 0, len(counts))
	total := 0
	for tagType, count := range counts {
		if _, existing := s.tagTypes[tagType]; !existing && len(s.tagTypes) >= maxTagTypes {
			s.dropped++

			continue
		}
		s.tagTypes[tagType] = count
		types = append(types, tagType)
		total += count
	}
	if len(types) == 0 {
		return
	}
	slices.Sort(types)

	event.Emit(c, event.RegistryTagsReceived{Types: types, Tags: total})
}

// CommandsReceived records the command tree's size. The tree itself is a wire
// structure this milestone does not model.
func (s *Registries) CommandsReceived(c *event.Collector, nodes int) {
	s.commandNodes, s.commandsKnown = nodes, true

	event.Emit(c, event.RegistryCommandsReceived{Nodes: nodes})
}

// PlayerListChange is one player's update. Every field says whether it was
// supplied, because protocol 775's action is a bitfield: one packet updates
// latency for one player and the display name of another, and an unsupplied
// field must not blank what an earlier packet set.
type PlayerListChange struct {
	UUID string

	Name    string
	SetName bool

	GameMode    int32
	SetGameMode bool

	Latency    int32
	SetLatency bool

	Listed    bool
	SetListed bool
}

// PlayerListChanged merges the server's player list.
//
// Adding and updating are one call because 775's action is a bitfield and one
// packet can add a player and update another's latency at once, where protocol
// 47's action is a single choice. Both reduce to the same three lists.
func (s *Registries) PlayerListChanged(
	c *event.Collector,
	changes []PlayerListChange,
	removed []string,
) {
	var added, updated []string
	for _, change := range changes {
		player, known := s.players[change.UUID]
		if !known {
			if len(s.players) >= maxListedPlayers {
				s.droppedPlayers++

				continue
			}
			player = ListedPlayer{UUID: change.UUID}
		}
		if change.SetName {
			player.Name = change.Name
		}
		if change.SetGameMode {
			player.GameMode = change.GameMode
		}
		if change.SetLatency {
			player.Latency = change.Latency
		}
		if change.SetListed {
			player.Listed = change.Listed
		}
		s.players[change.UUID] = player

		if known {
			updated = append(updated, change.UUID)
		} else {
			added = append(added, change.UUID)
		}
	}

	var gone []string
	for _, uuid := range removed {
		if _, known := s.players[uuid]; !known {
			continue
		}
		delete(s.players, uuid)
		gone = append(gone, uuid)
	}

	if len(added)+len(updated)+len(gone) == 0 {
		return
	}
	slices.Sort(added)
	slices.Sort(updated)
	slices.Sort(gone)

	event.Emit(c, event.RegistryPlayerListChanged{Added: added, Updated: updated, Removed: gone})
}

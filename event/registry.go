package event

// The registry domain is what the server said about its own vocabulary: the
// registries it defined for this connection, the tags it grouped them into,
// the commands it accepts, and the players on its list.
//
// **Session registry data overrides the generated registry for this
// connection.** That is the whole point of the domain. A modded server defines
// entity types, block types, and menus the generated data has never heard of,
// and a client that resolved a numeric ID through its own tables would name
// the wrong thing. The generated data stays reachable for lookups that do not
// depend on server configuration.

// RegistryDataReceived reports one registry the server defined.
//
// Protocol 47 has no registry-data packet at all — its registries are entirely
// static — so this never fires there, and the snapshot reports that rather
// than presenting an empty session registry as if the server had sent one.
//
// Registry data arrives in the configuration state, before play. It reaches
// this domain because the client owns the configuration phase rather than
// letting the login negotiator consume it.
type RegistryDataReceived struct {
	Stamp

	// Registry is the namespaced registry key, kept as sent — a modded server
	// defines registries with namespaces this client has never seen.
	Registry string
	// Entries is how many entries the registry declared.
	Entries int
	// State is the protocol state the packet arrived in, which is
	// "configuration" for every vanilla registry.
	State string
}

func (RegistryDataReceived) Name() Name     { return NameRegistryDataReceived }
func (RegistryDataReceived) Domain() Domain { return DomainRegistry }

// RegistryTagsReceived reports the tags the server grouped a registry's
// entries into.
type RegistryTagsReceived struct {
	Stamp

	// Types names the tag types this packet carried, such as
	// "minecraft:block".
	Types []string
	Tags  int
}

func (RegistryTagsReceived) Name() Name     { return NameRegistryTagsReceived }
func (RegistryTagsReceived) Domain() Domain { return DomainRegistry }

// RegistryCommandsReceived reports the command tree the server accepts.
// Protocol 47 has no command packet; 1.8 sends no tree at all.
type RegistryCommandsReceived struct {
	Stamp

	Nodes int
}

func (RegistryCommandsReceived) Name() Name     { return NameRegistryCommandsReceived }
func (RegistryCommandsReceived) Domain() Domain { return DomainRegistry }

// RegistryPlayerListChanged reports players joining, leaving, or changing on
// the server's list.
//
// The list is not the entity store. A player on the list may be nowhere near
// this client and have no entity, and a player entity in view may have left
// the list — the two describe different things and the taxonomy keeps them
// apart.
type RegistryPlayerListChanged struct {
	Stamp

	// Added, Updated, and Removed are player UUIDs. One packet can do all
	// three on 775, whose action is a bitfield rather than a single choice.
	Added   []string
	Updated []string
	Removed []string
}

func (RegistryPlayerListChanged) Name() Name     { return NameRegistryPlayerListChanged }
func (RegistryPlayerListChanged) Domain() Domain { return DomainRegistry }

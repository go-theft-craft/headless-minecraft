package event

// The chat domain is chat and presentational UI: messages, titles, boss bars,
// scoreboards, teams, advancements, sounds, statistics, dialogs, and
// completions.
//
// **Nothing here renders a chat component.** Both protocols send most of this
// text as a structured component — JSON on protocol 47, NBT on 775 — and
// turning one into a line of text is a presentation decision this library does
// not make for a consumer. What the events carry is the plain text where a
// protocol sends plain text, and otherwise the fact that a message arrived,
// with the raw packet reachable for a caller that wants to render it.

// ChatKind discriminates the three kinds of message rather than declaring
// three events for one fact.
type ChatKind string

const (
	// ChatKindPlayer is a message a player sent, signed on protocol 775.
	ChatKindPlayer ChatKind = "player"
	// ChatKindSystem is a message the server sent as itself.
	ChatKindSystem ChatKind = "system"
	// ChatKindProfileless is a message with a sender name but no player
	// profile behind it, which protocol 47 has no equivalent for.
	ChatKindProfileless ChatKind = "profileless"
)

// ChatReceived reports a message.
type ChatReceived struct {
	Stamp

	Kind ChatKind
	// Text is the message, when the protocol sends one as text. Protocol 775's
	// player chat carries a plain message alongside its component; protocol 47
	// and 775's system chat send a component only, and this is empty for them.
	Text string
	// Sender is the sending player's UUID on protocol 775's player chat, and
	// empty everywhere else.
	Sender string
	// Index is 775's global message index, which is what the removal packet
	// names. IndexKnown is false on protocol 47, which cannot remove a message
	// at all.
	Index      int32
	IndexKnown bool
	// Signed reports that the message arrived with a signature. **It is not a
	// claim that the signature is valid.** Validating chat signatures is not
	// something this milestone does, and saying otherwise would be worse than
	// not doing it.
	Signed bool
	// ActionBar reports 775's system chat asking to be shown on the action bar
	// rather than in the chat log.
	ActionBar bool
}

func (ChatReceived) Name() Name     { return NameChatReceived }
func (ChatReceived) Domain() Domain { return DomainChat }

// ChatRemoved reports the server withdrawing a message. The message is removed
// from the log, not marked: a caller reading the log must not see it.
//
// Protocol 47 has no such packet, so this never fires there.
type ChatRemoved struct {
	Stamp

	Index int32
}

func (ChatRemoved) Name() Name     { return NameChatRemoved }
func (ChatRemoved) Domain() Domain { return DomainChat }

// ChatTitleChanged reports the title, subtitle, or timing changing, or the
// titles being cleared.
type ChatTitleChanged struct {
	Stamp

	// Cleared reports that the server cleared the titles rather than setting
	// one.
	Cleared bool
	// Reset reports 775's harder clear, which also resets the timings.
	Reset bool

	FadeIn, Stay, FadeOut int32
	TimesKnown            bool
}

func (ChatTitleChanged) Name() Name     { return NameChatTitleChanged }
func (ChatTitleChanged) Domain() Domain { return DomainChat }

// ChatActionBarChanged reports the action-bar text changing.
type ChatActionBarChanged struct{ Stamp }

func (ChatActionBarChanged) Name() Name     { return NameChatActionBarChanged }
func (ChatActionBarChanged) Domain() Domain { return DomainChat }

// ChatBossBarChanged reports a boss bar appearing, changing, or going away.
//
// Protocol 47 has no boss-bar packet: a 1.8 boss bar is a wither entity with a
// name and a health bar, which arrives through the entity domain. This never
// fires on 47.
type ChatBossBarChanged struct {
	Stamp

	UUID string
	// Removed reports the bar going away.
	Removed bool
	// Health is the bar's fill. HealthKnown is false for the actions that do
	// not carry one.
	Health      float32
	HealthKnown bool
}

func (ChatBossBarChanged) Name() Name     { return NameChatBossBarChanged }
func (ChatBossBarChanged) Domain() Domain { return DomainChat }

// ChatScoreboardChanged reports an objective, a score, or the displayed
// objective changing.
type ChatScoreboardChanged struct {
	Stamp

	// Objective is the objective's name, and Entry the scoreboard entry a
	// score belongs to. Either may be empty depending on what changed.
	Objective string
	Entry     string
	Value     int32
	// Removed reports a score or objective being taken away.
	Removed bool
	// Displayed reports the packet that chooses which objective is shown, and
	// Position where.
	Displayed bool
	Position  int32
}

func (ChatScoreboardChanged) Name() Name     { return NameChatScoreboardChanged }
func (ChatScoreboardChanged) Domain() Domain { return DomainChat }

// ChatTeamsChanged reports a team being created, changed, removed, or having
// its membership altered.
type ChatTeamsChanged struct {
	Stamp

	Team string
	// Mode is the server's own name or number for what changed, kept as sent.
	Mode string
	// Players names the members this packet carried, if any.
	Players []string
	Removed bool
}

func (ChatTeamsChanged) Name() Name     { return NameChatTeamsChanged }
func (ChatTeamsChanged) Domain() Domain { return DomainChat }

// ChatAdvancementsChanged reports the advancement tree or a caller's progress
// through it. Protocol 47 has no advancements: 1.8 has achievements, which
// arrive as statistics.
type ChatAdvancementsChanged struct {
	Stamp

	Added   int
	Removed int
	// Reset reports the server replacing the tree rather than adding to it.
	Reset bool
}

func (ChatAdvancementsChanged) Name() Name     { return NameChatAdvancementsChanged }
func (ChatAdvancementsChanged) Domain() Domain { return DomainChat }

// ChatSoundPlayed reports a sound.
//
// It carries no stored state and exists so a caller can hear what the server
// is doing — a sound is often the only announcement of an event with no packet
// of its own.
type ChatSoundPlayed struct {
	Stamp

	// Sound is the sound's name where the protocol sends one. Protocol 775
	// sends a registry reference that may carry no name, and this is empty
	// then.
	Sound string
	// EntityID names the entity a sound follows, for the packet that has one.
	EntityID    int32
	EntityKnown bool
	// X, Y, Z is the sound's position in blocks, for the positional packet.
	X, Y, Z    float64
	Positioned bool
	Volume     float32
	Pitch      float32
	// Stopped reports 775's stop-sound packet rather than a sound starting.
	Stopped bool
}

func (ChatSoundPlayed) Name() Name     { return NameChatSoundPlayed }
func (ChatSoundPlayed) Domain() Domain { return DomainChat }

// ChatStatisticsReceived reports the statistics the server keeps for this
// player.
type ChatStatisticsReceived struct {
	Stamp

	Entries int
}

func (ChatStatisticsReceived) Name() Name     { return NameChatStatisticsReceived }
func (ChatStatisticsReceived) Domain() Domain { return DomainChat }

// ChatDialogShown reports the server opening or closing a dialog. Protocol 47
// has no dialog packet.
type ChatDialogShown struct {
	Stamp

	// Cleared reports the dialog closing rather than opening.
	Cleared bool
}

func (ChatDialogShown) Name() Name     { return NameChatDialogShown }
func (ChatDialogShown) Domain() Domain { return DomainChat }

// ChatTabCompleted reports the server answering a completion request.
type ChatTabCompleted struct {
	Stamp

	Matches []string
	// TransactionID correlates the answer with the request on protocol 775.
	// Protocol 47 sends no transaction, so TransactionKnown is false there.
	TransactionID    int32
	TransactionKnown bool
}

func (ChatTabCompleted) Name() Name     { return NameChatTabCompleted }
func (ChatTabCompleted) Domain() Domain { return DomainChat }

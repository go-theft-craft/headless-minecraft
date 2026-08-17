package world

import (
	"maps"
	"slices"

	"github.com/go-theft-craft/headless-minecraft/event"
)

// The chat domain holds messages and the presentational UI around them.
//
// It was M7's declared cut line, kept because the cost turned out to be one
// bounded log and a handful of scalars. Nothing here renders a chat component:
// both protocols send most of this text as a structured value, and turning one
// into a line of text is a presentation decision the library leaves to the
// caller.

const (
	// maxChatLog bounds the messages the log keeps. A busy public server sends
	// several a second, and a week-long session must not keep them all.
	maxChatLog = 256
	// maxBossBars bounds the boss bars one connection may accumulate.
	maxBossBars = 64
	// maxObjectives bounds the scoreboard objectives, and maxScores the
	// entries under one objective.
	maxObjectives = 64
	maxScores     = 1024
	// maxTeams bounds the teams.
	maxTeams = 256
)

// Message is one message in the log.
type Message struct {
	Kind event.ChatKind
	// Text is the plain message where the protocol sends one, and empty where
	// it sends a component only.
	Text   string
	Sender string
	// Index is protocol 775's global message index, which is what a removal
	// names. IndexKnown is false on protocol 47.
	Index      int32
	IndexKnown bool
	// Signed reports that a signature arrived. It is not a claim that the
	// signature is valid; nothing here validates one.
	Signed bool
}

// BossBar is one boss bar's state.
type BossBar struct {
	UUID        string
	Health      float32
	HealthKnown bool
}

// Objective is one scoreboard objective and the scores under it.
type Objective struct {
	Name string
	// Scores is each entry's value, by entry name.
	Scores map[string]int32
	// Displayed reports whether the server chose this objective for a display
	// position, and Position which.
	Displayed bool
	Position  int32
}

// Team is one team.
type Team struct {
	Name    string
	Players []string
}

// Chat is the message log and the UI around it.
type Chat struct {
	log          []Message
	droppedChat  int
	bossBars     map[string]BossBar
	objectives   map[string]Objective
	teams        map[string]Team
	dropped      int
	titleFadeIn  int32
	titleStay    int32
	titleFadeOut int32
	timesKnown   bool
	statistics   int
	statsKnown   bool
	dialogOpen   bool
}

// ChatView is the chat half of a snapshot. Its slices and maps are owned
// copies.
type ChatView struct {
	// Log is the messages the connection has seen, oldest first, bounded.
	Log []Message
	// DroppedMessages counts messages the bound pushed out of the log.
	DroppedMessages int

	BossBars   map[string]BossBar
	Objectives map[string]Objective
	Teams      map[string]Team
	Dropped    int

	FadeIn, Stay, FadeOut int32
	TitleTimesKnown       bool

	Statistics      int
	StatisticsKnown bool
	DialogOpen      bool
}

func newChat() *Chat {
	return &Chat{
		bossBars:   make(map[string]BossBar),
		objectives: make(map[string]Objective),
		teams:      make(map[string]Team),
	}
}

func (s *Chat) view() ChatView {
	objectives := make(map[string]Objective, len(s.objectives))
	for name, objective := range s.objectives {
		objective.Scores = maps.Clone(objective.Scores)
		objectives[name] = objective
	}
	teams := make(map[string]Team, len(s.teams))
	for name, team := range s.teams {
		team.Players = slices.Clone(team.Players)
		teams[name] = team
	}

	return ChatView{
		Log:             slices.Clone(s.log),
		DroppedMessages: s.droppedChat,
		BossBars:        maps.Clone(s.bossBars),
		Objectives:      objectives,
		Teams:           teams,
		Dropped:         s.dropped,
		FadeIn:          s.titleFadeIn, Stay: s.titleStay, FadeOut: s.titleFadeOut,
		TitleTimesKnown: s.timesKnown,
		Statistics:      s.statistics, StatisticsKnown: s.statsKnown,
		DialogOpen: s.dialogOpen,
	}
}

// Received appends a message to the log, dropping the oldest when the log is
// full.
func (s *Chat) Received(c *event.Collector, message Message, actionBar bool) {
	s.log = append(s.log, message)
	if len(s.log) > maxChatLog {
		// Reslicing rather than trimming in place: the log is handed out as a
		// clone, so the dropped head is not reachable and will be collected.
		s.log = slices.Clone(s.log[len(s.log)-maxChatLog:])
		s.droppedChat++
	}

	event.Emit(c, event.ChatReceived{
		Kind: message.Kind, Text: message.Text, Sender: message.Sender,
		Index: message.Index, IndexKnown: message.IndexKnown,
		Signed: message.Signed, ActionBar: actionBar,
	})
}

// Removed withdraws a message from the log.
//
// It is removed, not marked: a caller reading the log must not see a message
// the server withdrew. Removing one the log no longer holds is not an error —
// the bound may have dropped it first.
func (s *Chat) Removed(c *event.Collector, index int32) {
	s.log = slices.DeleteFunc(s.log, func(m Message) bool {
		return m.IndexKnown && m.Index == index
	})

	event.Emit(c, event.ChatRemoved{Index: index})
}

// TitleChanged records a title, subtitle, or timing. The text itself is a
// component the library does not render, so what the snapshot keeps is the
// timing and whether the titles were cleared.
func (s *Chat) TitleChanged(c *event.Collector, changed event.ChatTitleChanged) {
	if changed.TimesKnown {
		s.titleFadeIn, s.titleStay, s.titleFadeOut = changed.FadeIn, changed.Stay, changed.FadeOut
		s.timesKnown = true
	}
	if changed.Reset {
		s.timesKnown = false
	}

	event.Emit(c, changed)
}

// ActionBarChanged records the action-bar text changing.
func (s *Chat) ActionBarChanged(c *event.Collector) {
	event.Emit(c, event.ChatActionBarChanged{})
}

// BossBarChanged records a boss bar appearing, changing, or going away.
func (s *Chat) BossBarChanged(c *event.Collector, bar event.ChatBossBarChanged) {
	switch {
	case bar.Removed:
		delete(s.bossBars, bar.UUID)

	default:
		existing, known := s.bossBars[bar.UUID]
		if !known {
			if len(s.bossBars) >= maxBossBars {
				s.dropped++

				return
			}
			existing = BossBar{UUID: bar.UUID}
		}
		if bar.HealthKnown {
			existing.Health, existing.HealthKnown = bar.Health, true
		}
		s.bossBars[bar.UUID] = existing
	}

	event.Emit(c, bar)
}

// ObjectiveChanged records a scoreboard objective being created, changed, or
// removed.
func (s *Chat) ObjectiveChanged(c *event.Collector, name string, removed bool) {
	if removed {
		delete(s.objectives, name)
	} else if _, known := s.objectives[name]; !known {
		if len(s.objectives) >= maxObjectives {
			s.dropped++

			return
		}
		s.objectives[name] = Objective{Name: name, Scores: make(map[string]int32)}
	}

	event.Emit(c, event.ChatScoreboardChanged{Objective: name, Removed: removed})
}

// ScoreChanged records one entry's score under an objective.
//
// The objective is created if the client has no packet for it: a server sends
// scores for objectives a reconnecting client never saw declared.
func (s *Chat) ScoreChanged(c *event.Collector, objective, entry string, value int32, removed bool) {
	target, known := s.objectives[objective]
	if !known {
		if len(s.objectives) >= maxObjectives {
			s.dropped++

			return
		}
		target = Objective{Name: objective, Scores: make(map[string]int32)}
		s.objectives[objective] = target
	}

	switch {
	case removed:
		delete(target.Scores, entry)
	case len(target.Scores) >= maxScores:
		if _, existing := target.Scores[entry]; !existing {
			s.dropped++

			return
		}

		fallthrough
	default:
		target.Scores[entry] = value
	}

	event.Emit(c, event.ChatScoreboardChanged{
		Objective: objective, Entry: entry, Value: value, Removed: removed,
	})
}

// ObjectiveDisplayed records the server choosing which objective is shown
// where. An empty name clears the position.
func (s *Chat) ObjectiveDisplayed(c *event.Collector, name string, position int32) {
	for key, objective := range s.objectives {
		if objective.Position == position && objective.Displayed {
			objective.Displayed = false
			s.objectives[key] = objective
		}
	}
	if objective, known := s.objectives[name]; known {
		objective.Displayed, objective.Position = true, position
		s.objectives[name] = objective
	}

	event.Emit(c, event.ChatScoreboardChanged{
		Objective: name, Displayed: true, Position: position,
	})
}

// TeamChanged records a team being created, changed, or removed, and the
// members a packet carried.
func (s *Chat) TeamChanged(c *event.Collector, changed event.ChatTeamsChanged) {
	switch {
	case changed.Removed:
		delete(s.teams, changed.Team)

	default:
		team, known := s.teams[changed.Team]
		if !known {
			if len(s.teams) >= maxTeams {
				s.dropped++

				return
			}
			team = Team{Name: changed.Team}
		}
		if len(changed.Players) > 0 {
			team.Players = append(team.Players, changed.Players...)
			slices.Sort(team.Players)
			team.Players = slices.Compact(team.Players)
		}
		s.teams[changed.Team] = team
	}

	event.Emit(c, changed)
}

// AdvancementsChanged records the advancement tree changing.
func (s *Chat) AdvancementsChanged(c *event.Collector, changed event.ChatAdvancementsChanged) {
	event.Emit(c, changed)
}

// SoundPlayed records a sound. It stores nothing: a sound is an announcement,
// not state.
func (s *Chat) SoundPlayed(c *event.Collector, sound event.ChatSoundPlayed) {
	event.Emit(c, sound)
}

// StatisticsReceived records how many statistics the server keeps.
func (s *Chat) StatisticsReceived(c *event.Collector, entries int) {
	s.statistics, s.statsKnown = entries, true

	event.Emit(c, event.ChatStatisticsReceived{Entries: entries})
}

// DialogShown records the server opening or closing a dialog.
func (s *Chat) DialogShown(c *event.Collector, cleared bool) {
	s.dialogOpen = !cleared

	event.Emit(c, event.ChatDialogShown{Cleared: cleared})
}

// TabCompleted records the server answering a completion request. It stores
// nothing: a completion belongs to the request that asked for it.
func (s *Chat) TabCompleted(c *event.Collector, completed event.ChatTabCompleted) {
	event.Emit(c, completed)
}

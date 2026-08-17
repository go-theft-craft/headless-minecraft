package main

import (
	"fmt"
	"strings"

	"github.com/go-theft-craft/headless-minecraft/event"
	"github.com/go-theft-craft/headless-minecraft/world"
)

// describe renders one event as a line.
//
// It is a pure function of the event so that the whole printing half of this
// example is testable without a server, which is the only part of it worth
// testing: the rest is the library's.
//
// The default branch is not a fallback that should never run. Most of the
// taxonomy's events carry nothing worth putting on a line, and printing the
// name alone is the honest rendering for them.
func describe(e event.Event) string {
	switch value := e.(type) {
	case event.PlayerSpawned:
		return fmt.Sprintf("entity %d in %s mode %d", value.EntityID, value.Dimension, value.GameMode)

	case event.PlayerMoved:
		return fmt.Sprintf("%.2f %.2f %.2f", value.X, value.Y, value.Z)

	case event.PlayerHealthChanged:
		return fmt.Sprintf("health %.1f food %d", value.Health, value.Food)

	case event.PlayerDamaged:
		return describeDamage(value.Damage)

	case event.PlayerDied:
		if value.Attributed {
			return fmt.Sprintf("killed by entity %d", value.KillerID)
		}

		return "killed by nothing the protocol named"

	case event.EntitySpawned:
		return fmt.Sprintf("%d is %s at %.1f %.1f %.1f",
			value.EntityID, value.Type, value.X, value.Y, value.Z)

	case event.EntityMoved:
		return fmt.Sprintf("%d to %.2f %.2f %.2f", value.EntityID, value.X, value.Y, value.Z)

	case event.EntityRemoved:
		return fmt.Sprintf("%d", value.EntityID)

	case event.EntityDamaged:
		return fmt.Sprintf("%d %s", value.EntityID, describeDamage(value.Damage))

	case event.EntityDied:
		if value.Attributed {
			return fmt.Sprintf("%d killed by %d", value.EntityID, value.KillerID)
		}

		return fmt.Sprintf("%d", value.EntityID)

	case event.WorldChunkLoaded:
		return fmt.Sprintf("%d,%d with %d sections", value.X, value.Z, value.Sections)

	case event.WorldChunkUnloaded:
		return fmt.Sprintf("%d,%d", value.X, value.Z)

	case event.WorldBlocksChanged:
		return fmt.Sprintf("%d blocks, %d dropped", len(value.Positions), value.Dropped)

	case event.WorldWeatherChanged:
		if value.Raining {
			return fmt.Sprintf("raining, level %.2f", value.RainLevel)
		}

		return "clear"

	case event.WorldTimeChanged:
		if value.TimeOfDayKnown {
			return fmt.Sprintf("age %d, time %d", value.Age, value.TimeOfDay)
		}

		return fmt.Sprintf("age %d, %d clocks", value.Age, len(value.Clocks))

	case event.ContainerOpened:
		return fmt.Sprintf("%d is %s", value.ContainerID, value.MenuType)

	case event.ContainerClosed:
		return fmt.Sprintf("%d", value.ContainerID)

	case event.ContainerSlotsChanged:
		return fmt.Sprintf("%d: %d slots, %d properties",
			value.ContainerID, len(value.Slots), len(value.Properties))

	case event.RegistryDataReceived:
		return fmt.Sprintf("%s with %d entries in %s", value.Registry, value.Entries, value.State)

	case event.RegistryPlayerListChanged:
		return fmt.Sprintf("+%d ~%d -%d",
			len(value.Added), len(value.Updated), len(value.Removed))

	case event.ChatReceived:
		// The text is empty for every message either protocol sends only as a
		// component, which the library does not render.
		if value.Text != "" {
			return fmt.Sprintf("%s: %s", value.Kind, value.Text)
		}

		return fmt.Sprintf("%s message, not rendered", value.Kind)

	default:
		return ""
	}
}

func describeDamage(damage event.Damage) string {
	parts := make([]string, 0, 3)
	if damage.Typed {
		parts = append(parts, fmt.Sprintf("type %d", damage.TypeID))
	}
	if damage.Attributed {
		parts = append(parts, fmt.Sprintf("from entity %d", damage.CauseID))
	}
	if damage.Direct && damage.DirectID != damage.CauseID {
		parts = append(parts, fmt.Sprintf("dealt by %d", damage.DirectID))
	}
	if len(parts) == 0 {
		// Protocol 47 sends no source of any kind, and the event says so
		// rather than naming entity 0.
		return "unattributed"
	}

	return strings.Join(parts, ", ")
}

// summarize renders the final snapshot's counts, which is how a run says what
// it ended up holding rather than only what went past.
func summarize(snapshot world.Snapshot) string {
	return fmt.Sprintf(
		"revision %d: %d entities, %d chunks, %d menus, %d registries, %d messages",
		snapshot.Revision,
		len(snapshot.Entities.Tracked),
		len(snapshot.Chunks.Loaded),
		len(snapshot.Containers.Open),
		len(snapshot.Registries.Defined),
		len(snapshot.Chat.Log),
	)
}

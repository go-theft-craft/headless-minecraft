// Package java assembles the built-in Java Edition profiles.
//
// It is the one package that imports the per-version adapters, so a consumer
// linking only one version does not pull in the other's generated code.
package java

import (
	protocol "github.com/go-theft-craft/minecraft-protocol"
	gen1_8 "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"
	gen26_1 "github.com/go-theft-craft/minecraft-protocol/generated/java/v26_1"

	"github.com/go-theft-craft/headless-minecraft/event"
	adapter1_8 "github.com/go-theft-craft/headless-minecraft/internal/adapter/v1_8"
	adapter26_1 "github.com/go-theft-craft/headless-minecraft/internal/adapter/v26_1"
	"github.com/go-theft-craft/headless-minecraft/version"
)

// Java1_8 returns the protocol 47 profile with its own collector.
func Java1_8() version.WireProfile { return Java1_8With(new(event.Collector)) }

// Java1_8With returns the protocol 47 profile bound to a caller's collector.
// The client uses this form so its loop owns the collector it resets per
// batch.
func Java1_8With(collector *event.Collector) version.WireProfile {
	return version.WireProfile{
		ID:        adapter1_8.ProtocolID,
		Protocol:  gen1_8.Protocol(),
		Adapter:   adapter1_8.New(collector),
		Limits:    defaultLimits(),
		Readiness: adapter1_8.Readiness(),
		Collector: collector,
	}
}

// Current returns the current stable Java profile, protocol 775.
func Current() version.WireProfile { return CurrentWith(new(event.Collector)) }

// CurrentWith returns the protocol 775 profile bound to a collector.
func CurrentWith(collector *event.Collector) version.WireProfile {
	return version.WireProfile{
		ID:        adapter26_1.ProtocolID,
		Protocol:  gen26_1.Protocol(),
		Adapter:   adapter26_1.New(collector),
		Limits:    defaultLimits(),
		Readiness: adapter26_1.Readiness(),
		Collector: collector,
	}
}

// BundleDelimiter reports the packet name that opens and closes a bundle for
// one profile, or the empty string when the protocol does not bundle.
func BundleDelimiter(id string) string {
	switch id {
	case adapter26_1.ProtocolID:
		return adapter26_1.BundleDelimiter
	default:
		return adapter1_8.BundleDelimiter
	}
}

func defaultLimits() protocol.Limits {
	limits, err := protocol.NewLimits()
	if err != nil {
		// NewLimits with no options returns validated defaults, so this
		// cannot fail. Panicking here rather than returning an error keeps
		// the profile constructors total.
		panic("java: default limits are invalid: " + err.Error())
	}

	return limits
}

package v26_1

import (
	"context"
	"math"
	"slices"
	"strconv"
	"strings"

	protocol "github.com/go-theft-craft/minecraft-protocol"
	gen "github.com/go-theft-craft/minecraft-protocol/generated/java/v26_1"

	"github.com/go-theft-craft/headless-minecraft/event"
)

// Packet names are shared across states in protocol 775: keep_alive,
// custom_payload, cookie_request, store_cookie, transfer, and the resource
// pack packets all exist in both configuration and play, with a distinct
// generated type in each. A handler is registered once per name and tells the
// two apart by the value's type, which is also what makes it safe against a
// packet that arrived undecoded.

// handlerFunc adapts a function to version.Handler.
type handlerFunc func(context.Context, protocol.Packet) error

func (f handlerFunc) Handle(ctx context.Context, p protocol.Packet) error { return f(ctx, p) }

func (a adapter) handlers() map[string]handlerFunc {
	return map[string]handlerFunc{
		"keep_alive":             a.keepAlive,
		"position":               a.position,
		"chunk_batch_finished":   a.chunkBatchFinished,
		"custom_payload":         a.customPayload,
		"kick_disconnect":        a.disconnect,
		"disconnect":             a.disconnect,
		"transfer":               a.transfer,
		"add_resource_pack":      a.resourcePackOffered,
		"remove_resource_pack":   a.resourcePackRevoked,
		"cookie_request":         a.cookieRequested,
		"store_cookie":           a.cookieStored,
		"server_data":            a.serverData,
		"server_links":           a.serverLinks,
		"feature_flags":          a.featureFlags,
		"custom_report_details":  a.reportDetails,
		"low_disk_space_warning": a.lowDiskSpace,
		"select_known_packs":     a.selectKnownPacks,
		"finish_configuration":   a.finishConfiguration,
	}
}

// keepAlive answers in whichever state the keepalive arrived in. A server
// drops a client that stays silent, and configuration is long enough on a
// modded server for that to happen before play is ever reached.
func (a adapter) keepAlive(_ context.Context, p protocol.Packet) error {
	var id int64
	var answer protocol.Packet
	switch value := p.Value.(type) {
	case *gen.PlayClientboundKeepAlive:
		id = value.KeepAliveID
		reply := &gen.PlayServerboundKeepAlive{KeepAliveID: id}
		answer = serverbound(gen.StatePlay, "keep_alive", reply.PacketID(), reply)
	case *gen.ConfigurationClientboundKeepAlive:
		id = value.KeepAliveID
		reply := &gen.ConfigurationServerboundKeepAlive{KeepAliveID: id}
		answer = serverbound(gen.StateConfiguration, "keep_alive", reply.PacketID(), reply)
	default:
		return nil
	}

	a.outbox.Add(answer)
	// Elapsed stays zero: measuring it needs the round trip, which the loop
	// owns, not the adapter.
	event.Emit(a.collector, event.KeepAlivePonged{ID: id})

	return nil
}

// position confirms a server-initiated teleport, every time one arrives.
//
// Protocol 775 places the player and corrects the player with the same packet,
// and each one carries a teleport ID the server expects back. Until that
// confirmation arrives the server discards everything the client says about
// where it is. A client that confirms only the placement is therefore accepted
// once and ignored from its first correction onward: it keeps walking in its
// own reckoning, the server keeps it where it put it, and the gap widens until
// the server reports the player moving impossibly fast and corrects it again.
// Nothing in that sequence looks like an error from the client's side, which is
// why it survived until a bot walked into it — the orbit example, against a
// vanilla 26.1.2 server, corrected six times without covering a step.
//
// It is a handler rather than part of the readiness rule because it is not a
// readiness question. Reaching play happens once; confirming a teleport is owed
// for the life of the connection, which is the same reason keepalives are
// answered here.
//
// The flags are not inspected. Whether the coordinates are absolute or relative
// decides how the world resolves the position, and the world decides that; the
// confirmation echoes an ID and is owed either way.
func (a adapter) position(_ context.Context, p protocol.Packet) error {
	value, ok := p.Value.(*gen.PlayClientboundPosition)
	if !ok {
		return nil
	}

	answer := &gen.PlayServerboundTeleportConfirm{TeleportID: value.TeleportID}
	a.outbox.Add(serverbound(gen.StatePlay, "teleport_confirm", answer.PacketID(), answer))

	return nil
}

// chunksPerTick is the rate this client asks a server to send chunks at.
//
// It is a request, not a measurement. Vanilla's client times how long it spent
// on the last batch and asks for a rate derived from it, so the number it sends
// climbs as it warms up: a real 26.1.2 client against the same flat server
// asked for 5.25 on its first batch and converged around 14.6. This client does
// not time itself — it decodes a chunk into memory and renders nothing, which
// is the work the rate exists to limit — so it names one number and keeps
// naming it.
//
// Eight sits inside the range the reference client asked for: above the cold
// start, so chunks do not trickle in, and below the ceiling it settled on, so a
// slow machine is not flooded by a claim this client never checked. Measuring
// it properly is worth doing, and is a different change from sending the packet
// at all.
const chunksPerTick = 8.0

// chunkBatchFinished acknowledges a batch of chunks, which is what asks for the
// next one.
//
// A 26.1 server streams chunks in batches under the client's control: it sends
// chunk_batch_start, the chunks, then chunk_batch_finished, and then it stops
// until the client answers with the rate it wants. The answer is the flow
// control. A client that never sends it receives exactly one batch for the life
// of the connection and no error, because from the server's side a client that
// has not acknowledged is a client that is still busy.
//
// What that looks like from outside is a bot that walks confidently to the edge
// of the chunks it was given and then stands still on open ground: everything
// beyond them is not solid, it is unknown, and the navigator will not step into
// a block it cannot classify. The orbit example did exactly this against a
// vanilla 26.1.2 server — nine chunks, one batch, and a bot that stopped at the
// boundary of the last one it had and reported itself blocked with nothing in
// front of it.
//
// It is a handler for the same reason the teleport confirmation is one: it is
// owed for the life of the connection, not settled once on the way in.
func (a adapter) chunkBatchFinished(_ context.Context, p protocol.Packet) error {
	if _, ok := p.Value.(*gen.PlayClientboundChunkBatchFinished); !ok {
		return nil
	}

	answer := &gen.PlayServerboundChunkBatchReceived{ChunksPerTick: chunksPerTick}
	a.outbox.Add(serverbound(
		gen.StatePlay, "chunk_batch_received", answer.PacketID(), answer,
	))

	return nil
}

// selectKnownPacks answers the question a 26.1 server stops on.
//
// No registry data and no finish handshake arrive until the client states
// which packs it already holds, and a connection that never answers looks
// perfectly healthy while it waits forever — the defect M4's live check
// found. The list is empty, which is the honest answer for a headless client
// that ships no pack data, and it is what the shared login exchange answers
// too.
func (a adapter) selectKnownPacks(_ context.Context, p protocol.Packet) error {
	if _, ok := p.Value.(*gen.ConfigurationClientboundSelectKnownPacks); !ok {
		return nil
	}

	answer := &gen.ConfigurationServerboundSelectKnownPacks{}
	a.outbox.Add(serverbound(
		gen.StateConfiguration, "select_known_packs", answer.PacketID(), answer,
	))

	return nil
}

// finishConfiguration acknowledges the end of configuration, which is what
// moves the connection into play.
func (a adapter) finishConfiguration(_ context.Context, p protocol.Packet) error {
	if _, ok := p.Value.(*gen.ConfigurationClientboundFinishConfiguration); !ok {
		return nil
	}

	answer := &gen.ConfigurationServerboundFinishConfiguration{}
	a.outbox.Add(serverbound(
		gen.StateConfiguration, "finish_configuration", answer.PacketID(), answer,
	))

	return nil
}

// serverbound addresses one answer. Every field matters: the state and
// direction pick the codec, and the name is what a capture, a log, or a
// PacketSent event identifies it by.
func serverbound(state protocol.State, name string, id int32, value any) protocol.Packet {
	return protocol.Packet{
		State:     state,
		Direction: protocol.DirectionServerbound,
		ID:        id,
		Name:      name,
		Value:     value,
	}
}

func (a adapter) customPayload(_ context.Context, p protocol.Packet) error {
	var channel string
	var data []byte
	switch value := p.Value.(type) {
	case *gen.PlayClientboundCustomPayload:
		channel, data = value.Channel, value.Data
	case *gen.ConfigurationClientboundCustomPayload:
		channel, data = value.Channel, value.Data
	default:
		return nil
	}
	event.Emit(a.collector, event.CustomPayloadReceived{
		Channel: channel,
		Payload: slices.Clone(data),
	})

	return nil
}

// disconnect reports a server ending the session, in play or configuration.
//
// The reason stays empty. Protocol 775 states it as a structured chat
// component, and rendering one to text is a presentation decision this
// package does not get to make on a consumer's behalf — the same choice the
// shared login exchange makes for its own disconnects.
func (a adapter) disconnect(_ context.Context, p protocol.Packet) error {
	switch p.Value.(type) {
	case *gen.PlayClientboundKickDisconnect, *gen.ConfigurationClientboundDisconnect:
	default:
		return nil
	}

	event.Emit(a.collector, event.Disconnected{
		Source: event.DisconnectByServer,
		State:  string(p.State),
	})

	return nil
}

func (a adapter) transfer(_ context.Context, p protocol.Packet) error {
	var host string
	var port int32
	switch value := p.Value.(type) {
	case *gen.PlayClientboundTransfer:
		host, port = value.Host, value.Port
	case *gen.ConfigurationClientboundTransfer:
		host, port = value.Host, value.Port
	default:
		return nil
	}

	// The wire type is wider than a port. A server that sends something that
	// is not one is reported with a zero port rather than a wrapped number.
	if port < 0 || port > math.MaxUint16 {
		port = 0
	}

	event.Emit(a.collector, event.TransferRequested{Host: host, Port: uint16(port)})

	return nil
}

func (a adapter) resourcePackOffered(_ context.Context, p protocol.Packet) error {
	var offered event.ResourcePackOffered
	switch value := p.Value.(type) {
	case *gen.PlayClientboundAddResourcePack:
		offered = event.ResourcePackOffered{
			UUID: value.UUID.String(), URL: value.URL, Hash: value.Hash, Required: value.Forced,
		}
	case *gen.ConfigurationClientboundAddResourcePack:
		offered = event.ResourcePackOffered{
			UUID: value.UUID.String(), URL: value.URL, Hash: value.Hash, Required: value.Forced,
		}
	default:
		return nil
	}

	event.Emit(a.collector, offered)

	return nil
}

// resourcePackRevoked reports a withdrawn pack. An absent UUID means every
// pack, which the event reports as an empty one.
func (a adapter) resourcePackRevoked(_ context.Context, p protocol.Packet) error {
	var revoked event.ResourcePackRevoked
	switch value := p.Value.(type) {
	case *gen.PlayClientboundRemoveResourcePack:
		if value.UUID != nil {
			revoked.UUID = value.UUID.String()
		}
	case *gen.ConfigurationClientboundRemoveResourcePack:
		if value.UUID != nil {
			revoked.UUID = value.UUID.String()
		}
	default:
		return nil
	}

	event.Emit(a.collector, revoked)

	return nil
}

func (a adapter) cookieRequested(_ context.Context, p protocol.Packet) error {
	var key string
	switch value := p.Value.(type) {
	case *gen.PlayClientboundCookieRequest:
		key = value.Cookie
	case *gen.ConfigurationClientboundCookieRequest:
		key = value.Cookie
	default:
		return nil
	}
	event.Emit(a.collector, event.CookieRequested{Key: key})

	return nil
}

// cookieStored reports the size of a stored cookie, never its bytes. A cookie
// is a server-issued token, and an event carrying it would put it in every
// subscriber's log.
func (a adapter) cookieStored(_ context.Context, p protocol.Packet) error {
	var stored event.CookieStored
	switch value := p.Value.(type) {
	case *gen.PlayClientboundStoreCookie:
		stored = event.CookieStored{Key: value.Key, Bytes: len(value.Value)}
	case *gen.ConfigurationClientboundStoreCookie:
		stored = event.CookieStored{Key: value.Key, Bytes: len(value.Value)}
	default:
		return nil
	}

	event.Emit(a.collector, stored)

	return nil
}

func (a adapter) serverData(_ context.Context, p protocol.Packet) error {
	value, ok := p.Value.(*gen.PlayClientboundServerData)
	if !ok {
		return nil
	}

	// The MOTD is a chat component and the icon is bytes; neither belongs in
	// a string map. What a subscriber can act on is whether they arrived.
	event.Emit(a.collector, event.ServerMetadataChanged{
		Kind:  "server_data",
		Value: map[string]string{"icon": strconv.FormatBool(value.IconBytes != nil)},
	})

	return nil
}

func (a adapter) serverLinks(_ context.Context, p protocol.Packet) error {
	var links int
	switch value := p.Value.(type) {
	case *gen.PlayClientboundServerLinks:
		links = len(value.Links)
	case *gen.ConfigurationClientboundServerLinks:
		links = len(value.Links)
	default:
		return nil
	}

	event.Emit(a.collector, event.ServerMetadataChanged{
		Kind:  "server_links",
		Value: map[string]string{"count": strconv.Itoa(links)},
	})

	return nil
}

func (a adapter) featureFlags(_ context.Context, p protocol.Packet) error {
	value, ok := p.Value.(*gen.ConfigurationClientboundFeatureFlags)
	if !ok {
		return nil
	}

	event.Emit(a.collector, event.ServerMetadataChanged{
		Kind:  "feature_flags",
		Value: map[string]string{"features": strings.Join(value.Features, ",")},
	})

	return nil
}

func (a adapter) reportDetails(_ context.Context, p protocol.Packet) error {
	details := make(map[string]string)
	switch value := p.Value.(type) {
	case *gen.PlayClientboundCustomReportDetails:
		for _, item := range value.Details {
			details[item.Key] = item.Value
		}
	case *gen.ConfigurationClientboundCustomReportDetails:
		for _, item := range value.Details {
			details[item.Key] = item.Value
		}
	default:
		return nil
	}

	event.Emit(a.collector, event.ServerMetadataChanged{
		Kind:  "custom_report_details",
		Value: details,
	})

	return nil
}

func (a adapter) lowDiskSpace(_ context.Context, p protocol.Packet) error {
	if _, ok := p.Value.(*gen.PlayClientboundLowDiskSpaceWarning); !ok {
		return nil
	}

	event.Emit(a.collector, event.ServerMetadataChanged{Kind: "low_disk_space_warning"})

	return nil
}

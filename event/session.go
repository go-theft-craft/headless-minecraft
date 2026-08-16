package event

import "time"

// Every session event embeds Stamp, which supplies Revision, and declares its
// own Name and Domain. They are value types: a subscriber holds a copy and
// cannot reach what another subscriber sees.

// Connecting reports that the client is about to dial. It carries no
// credential: the address is public and the identity is not.
type Connecting struct {
	Stamp

	Address string
}

func (Connecting) Name() Name     { return NameSessionConnecting }
func (Connecting) Domain() Domain { return DomainSession }

// Authenticated reports that the auth provider returned an identity.
type Authenticated struct {
	Stamp

	Username string
	UUID     string
}

func (Authenticated) Name() Name     { return NameSessionAuthenticated }
func (Authenticated) Domain() Domain { return DomainSession }

// StateChanged reports a protocol state transition in either direction,
// including the play-to-configuration return that protocol 775 permits.
type StateChanged struct {
	Stamp

	From string
	To   string
}

func (StateChanged) Name() Name     { return NameSessionStateChanged }
func (StateChanged) Domain() Domain { return DomainSession }

// Ready reports that the server will accept action packets. It is emitted
// once per connection, at the point Connect returns.
type Ready struct {
	Stamp

	EntityID  int32
	Dimension string
	GameMode  uint8
}

func (Ready) Name() Name     { return NameSessionReady }
func (Ready) Domain() Domain { return DomainSession }

// DisconnectSource says who ended the session.
type DisconnectSource string

const (
	// DisconnectByServer is a disconnect packet from the peer.
	DisconnectByServer DisconnectSource = "server"
	// DisconnectByTransport is a connection loss with no disconnect packet.
	DisconnectByTransport DisconnectSource = "transport"
)

// Disconnected reports that the session ended before Close was called.
type Disconnected struct {
	Stamp

	Source DisconnectSource
	Reason string
	State  string
}

func (Disconnected) Name() Name     { return NameSessionDisconnected }
func (Disconnected) Domain() Domain { return DomainSession }

// Closed reports that Close finished and every owned goroutine stopped. It
// is the last event a subscription receives.
type Closed struct {
	Stamp
}

func (Closed) Name() Name     { return NameSessionClosed }
func (Closed) Domain() Domain { return DomainSession }

// KeepAlivePonged reports that the client answered a keepalive.
type KeepAlivePonged struct {
	Stamp

	ID      int64
	Elapsed time.Duration
}

func (KeepAlivePonged) Name() Name     { return NameSessionKeepAlivePonged }
func (KeepAlivePonged) Domain() Domain { return DomainSession }

// TransferRequested reports a server asking the client to move to another
// host. The client never follows it: transferring repeats a connection, and
// the library does not reconnect on its own.
type TransferRequested struct {
	Stamp

	Host string
	Port uint16
}

func (TransferRequested) Name() Name     { return NameSessionTransferRequested }
func (TransferRequested) Domain() Domain { return DomainSession }

// ResourcePackOffered reports a pack the server offered. The client does not
// download it.
type ResourcePackOffered struct {
	Stamp

	UUID     string
	URL      string
	Hash     string
	Required bool
}

func (ResourcePackOffered) Name() Name     { return NameSessionResourcePackOffered }
func (ResourcePackOffered) Domain() Domain { return DomainSession }

// ResourcePackRevoked reports a pack the server withdrew.
type ResourcePackRevoked struct {
	Stamp

	UUID string
}

func (ResourcePackRevoked) Name() Name     { return NameSessionResourcePackRevoked }
func (ResourcePackRevoked) Domain() Domain { return DomainSession }

// ServerMetadataChanged reports server-describing data: server data, links,
// feature flags, report details, and low-disk warnings.
type ServerMetadataChanged struct {
	Stamp

	Kind  string
	Value map[string]string
}

func (ServerMetadataChanged) Name() Name     { return NameSessionServerMetadataChanged }
func (ServerMetadataChanged) Domain() Domain { return DomainSession }

// CookieRequested reports a server asking for a stored cookie.
type CookieRequested struct {
	Stamp

	Key string
}

func (CookieRequested) Name() Name     { return NameSessionCookieRequested }
func (CookieRequested) Domain() Domain { return DomainSession }

// CookieStored reports a server storing a cookie on the client.
type CookieStored struct {
	Stamp

	Key   string
	Bytes int
}

func (CookieStored) Name() Name     { return NameSessionCookieStored }
func (CookieStored) Domain() Domain { return DomainSession }

// CustomPayloadReceived reports a plugin message in configuration or play.
// Payload is an owned copy.
type CustomPayloadReceived struct {
	Stamp

	Channel string
	Payload []byte
}

func (CustomPayloadReceived) Name() Name     { return NameSessionCustomPayloadReceived }
func (CustomPayloadReceived) Domain() Domain { return DomainSession }

// PacketReceived carries one decoded inbound packet. It is delivered only to
// subscribers that selected DomainRaw.
//
// Packet is the wire packet's name. It cannot be called Name: that is the
// method every event owes the Event interface, and a struct cannot hold both.
type PacketReceived struct {
	Stamp

	State   string
	Packet  string
	ID      int32
	Bundled bool
}

func (PacketReceived) Name() Name     { return NameSessionPacketReceived }
func (PacketReceived) Domain() Domain { return DomainRaw }

// PacketSent carries one packet the client wrote.
type PacketSent struct {
	Stamp

	State  string
	Packet string
	ID     int32
}

func (PacketSent) Name() Name     { return NameSessionPacketSent }
func (PacketSent) Domain() Domain { return DomainRaw }

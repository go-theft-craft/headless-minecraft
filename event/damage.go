package event

// Damage is the attribution two domains share: the local player and every
// other entity take damage from the same kinds of source, so the shape is
// declared once and PlayerDamaged and EntityDamaged both carry it.
//
// **The protocols disagree about how much of this exists.** Protocol 775 sends
// a damage event naming the type, the entity responsible, the entity that
// actually dealt it, and sometimes a position. Protocol 47 has no damage packet
// at all: it reports being hurt as one of many entity statuses and says nothing
// about where the hurt came from. Nothing here fills that in. A caller that
// wants an attacker on protocol 47 infers one — from who swung, from who is
// nearest — and that inference is the caller's, because it is a guess and a
// guess presented as an observation is worse than an honest zero.
//
// Every "is it there" question is a separate boolean rather than a sentinel,
// because entity 0 is a legal entity and damage type 0 is a legal damage type.
type Damage struct {
	// TypeID is the server's own damage-type identifier, which indexes the
	// session's damage-type registry on protocol 775. Typed reports whether a
	// protocol sent one.
	TypeID int32
	Typed  bool

	// CauseID is the entity held responsible — the skeleton that fired, the
	// player that threw. It is the one a retaliating caller wants.
	CauseID int32
	// Attributed reports whether the server named a responsible entity.
	Attributed bool

	// DirectID is the entity that physically dealt the damage, which is the
	// arrow rather than the skeleton behind it. For a melee hit it equals
	// CauseID.
	DirectID int32
	// Direct reports whether the server named one.
	Direct bool

	// X, Y, Z is where the damage came from, which a server sends for damage
	// with no entity behind it at all. Positioned reports whether it did.
	X, Y, Z    float64
	Positioned bool
}

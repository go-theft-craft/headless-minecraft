package main

import "testing"

// TestBothVersionsNameWhatTheyMeet pins the lookup against the real data sets,
// through the type strings the adapters actually mint.
//
// The identifiers are the versions' own and they disagree -- a zombie is 54 in
// 47 and 150 in 775 -- which is the whole reason the index is built per
// version. A test that used one number for both would pass on the version it
// was written for and quietly name the wrong mob on the other.
func TestBothVersionsNameWhatTheyMeet(t *testing.T) {
	t.Parallel()

	for name, test := range map[string]struct {
		legacy  bool
		wire    string
		want    string
		pursues bool
	}{
		"a zombie in 775":  {false, "java/26.1:entity/150", "Zombie", true},
		"a zombie in 47":   {true, "java/1.8.9:mob/54", "Zombie", true},
		"a minecart in 47": {true, "java/1.8.9:object/10", "Minecart", false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			kinds, err := NewKinds(test.legacy)
			if err != nil {
				t.Fatalf("NewKinds: %v", err)
			}

			kind, named := kinds.Lookup(test.wire)
			if !named {
				t.Fatalf("%s is not named", test.wire)
			}
			if kind.Name != test.want {
				t.Errorf("%s is %q, want %q", test.wire, kind.Name, test.want)
			}
			if kind.Pursues != test.pursues {
				t.Errorf("%s pursues=%v, want %v", test.wire, kind.Pursues, test.pursues)
			}
		})
	}
}

// TestAnEntityWithNoDataIsNotAssumedHarmless pins the cautious default.
//
// A modded server spawns types no data set has heard of. "I do not know what
// hit me" has to read as something to run from, because the alternative is a
// bot that stands still for everything unfamiliar.
func TestAnEntityWithNoDataIsNotAssumedHarmless(t *testing.T) {
	t.Parallel()

	kinds, err := NewKinds(false)
	if err != nil {
		t.Fatalf("NewKinds: %v", err)
	}

	if _, named := kinds.Lookup("some_mod:entity/9001"); named {
		t.Error("named an entity no data set carries")
	}

	// And the core reads it as a threat. Named is the flag that decides it, so
	// the zero Kind must never be the thing that makes something harmless.
	var unknown Entity
	if unknown.Named {
		t.Error("an unresolved entity reports itself named")
	}
}

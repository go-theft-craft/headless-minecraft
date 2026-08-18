package java_test

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	protocol "github.com/go-theft-craft/minecraft-protocol"
	"github.com/go-theft-craft/minecraft-protocol/wire/java"

	"github.com/go-theft-craft/headless-minecraft/version"
	versionjava "github.com/go-theft-craft/headless-minecraft/version/java"
)

// The set-wide action gates live here rather than in version, because they need
// what only this package has: both adapters. version declares the intents and
// cannot see either encoder, and internal/adapter is two packages with no
// parent to hold a test. Keeping the list in one place is what makes the
// completeness check below worth anything — two lists would be two things to
// keep in step, which is the failure these gates exist to prevent.

// allActions returns one value of every exported action type.
//
// Adding an action without adding it here fails TestEveryActionTypeIsListed,
// which is the point: the coverage gate can only cover what it is given.
func allActions() []version.Action {
	at := version.Cursor{}

	return []version.Action{
		version.ActionMove{},
		version.ActionLook{},
		version.ActionMoveLook{},
		version.ActionGround{},
		version.ActionRespawn{},
		version.ActionInput{},
		version.ActionSprint{},
		version.ActionCommand{Command: "seed"},
		version.ActionHeldSlot{},
		version.ActionSwing{},
		version.ActionUseItem{},
		version.ActionUseOn{},
		version.ActionReleaseUse{},
		// Use-at is the one member that needs a field, and a gate that sent
		// the refusable form would prove only that the refusal works.
		version.ActionInteract{Kind: version.InteractUseAt, At: &at},
		version.ActionDig{},
		version.ActionClickSlot{Mode: version.ClickPickup},
		version.ActionDrop{},
		version.ActionCloseWindow{},
		version.ActionEntityAction{Kind: version.SprintStart},
		version.ActionSwapHands{},
		version.ActionChat{Message: "hello"},
	}
}

// adapters returns both built-in protocols, named.
func adapters() map[string]version.Adapter {
	return map[string]version.Adapter{
		"protocol 47":  versionjava.Java1_8().Adapter,
		"protocol 775": versionjava.Current().Adapter,
	}
}

func TestEveryActionTypeIsListed(t *testing.T) {
	t.Parallel()

	// Read off the source rather than off a registry, because there is no
	// registry: an action is an exported struct in version that implements
	// Action, and nothing collects them at run time. A gate that trusted the
	// list to be complete would pass on the day somebody forgot.
	listed := make(map[string]bool, len(allActions()))
	for _, action := range allActions() {
		listed[fmt.Sprintf("%T", action)] = true
	}

	for _, declared := range declaredActionTypes(t) {
		if !listed["version."+declared] {
			t.Errorf("version.%s is not in allActions, so no gate covers it", declared)
		}
	}
}

func TestEveryActionKindIsUniqueAndSnakeCase(t *testing.T) {
	t.Parallel()

	// A kind appears in errors and in logs, and two actions sharing one is two
	// intents a reader cannot tell apart after the fact.
	snakeCase := regexp.MustCompile(`^[a-z]+(_[a-z]+)*$`)

	seen := make(map[string]string)
	for _, action := range allActions() {
		kind := action.ActionKind()

		if previous, clash := seen[kind]; clash {
			t.Errorf("%T and %s share the kind %q", action, previous, kind)
		}
		if !snakeCase.MatchString(kind) {
			t.Errorf("%T has kind %q, want lower snake_case", action, kind)
		}

		seen[kind] = fmt.Sprintf("%T", action)
	}
}

func TestEveryActionEitherEncodesOrRefusesOnBothProtocols(t *testing.T) {
	t.Parallel()

	// The gate is not that every action works everywhere. It is that no action
	// falls through undecided: an adapter either encodes it or names it in a
	// refusal. Adding an action and forgetting one protocol fails here rather
	// than at run time on somebody's server.
	//
	// And an encoded packet has to write. A refusal is loud; a packet the
	// codec rejects is a write error on a live connection, which is why every
	// accepted action is put through the real encoder here.
	for name, adapter := range adapters() {
		for _, action := range allActions() {
			t.Run(fmt.Sprintf("%s/%s", name, action.ActionKind()), func(t *testing.T) {
				t.Parallel()

				packet, err := adapter.EncodeAction(action)
				if err != nil {
					if !errors.Is(err, version.ErrUnsupportedAction) {
						t.Fatalf("EncodeAction = %v, want nil or ErrUnsupportedAction", err)
					}
					if !strings.Contains(err.Error(), action.ActionKind()) {
						t.Fatalf("refusal %q does not name the kind %q", err, action.ActionKind())
					}

					return
				}

				if packet.Name == "" || packet.Value == nil {
					t.Fatalf("encoded an unaddressed packet: %+v", packet)
				}
				requireEncodable(t, packet.Value)
			})
		}
	}
}

func TestNoMilestoneNumbersLeakedIntoVersion(t *testing.T) {
	t.Parallel()

	// Break times, reach distances, and cooldowns belong to M9.4 through M9.6.
	// A name here for one of them is this package claiming a measurement it
	// never made.
	forbidden := []string{"BreakTime", "Reach", "Cooldown", "Hardness"}

	source := packageSource(t, "..")
	for _, name := range forbidden {
		if strings.Contains(source, name) {
			t.Errorf("version mentions %q, which belongs to a milestone gate", name)
		}
	}
}

// requireEncodable writes a packet body through its own generated codec.
func requireEncodable(t *testing.T, value any) {
	t.Helper()

	encoder, ok := value.(interface{ Encode(*java.Buffer) error })
	if !ok {
		t.Fatalf("%T has no generated encoder", value)
	}

	limits, err := protocol.NewLimits()
	if err != nil {
		t.Fatalf("NewLimits: %v", err)
	}
	buffer, err := java.NewWriteBuffer(limits)
	if err != nil {
		t.Fatalf("NewWriteBuffer: %v", err)
	}
	if err := encoder.Encode(buffer); err != nil {
		t.Fatalf("%T does not write: %v", value, err)
	}
}

// declaredActionTypes returns the exported action struct names version declares.
func declaredActionTypes(t *testing.T) []string {
	t.Helper()

	set := token.NewFileSet()

	var names []string
	for _, path := range packageFiles(t, "..") {
		file, err := parser.ParseFile(set, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}

		ast.Inspect(file, func(node ast.Node) bool {
			spec, ok := node.(*ast.TypeSpec)
			if !ok || !strings.HasPrefix(spec.Name.Name, "Action") {
				return true
			}
			if _, isStruct := spec.Type.(*ast.StructType); isStruct {
				names = append(names, spec.Name.Name)
			}

			return true
		})
	}

	if len(names) == 0 {
		t.Fatal("found no action types in version, which cannot be right")
	}

	return names
}

// packageFiles returns the non-test Go files in a directory.
func packageFiles(t *testing.T, dir string) []string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}

	var paths []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		paths = append(paths, filepath.Join(dir, name))
	}

	return paths
}

// packageSource returns every non-test Go file in a directory, concatenated.
func packageSource(t *testing.T, dir string) string {
	t.Helper()

	var source strings.Builder
	for _, path := range packageFiles(t, dir) {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		source.Write(content)
	}

	return source.String()
}

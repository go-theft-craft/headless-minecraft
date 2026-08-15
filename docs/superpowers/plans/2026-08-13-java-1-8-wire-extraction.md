# Java 1.8 wire extraction implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `subagent-driven-development` or execute this plan inline one task at a time. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** Extract the tested Java 1.8 wire primitives from `server` into `minecraft-protocol` without replacing the committed protocol envelope or resource-limit API.

**Architecture:** The module-root `protocol` package remains the edition-neutral contract. The new `wire/java` package owns Java VarInt, field, reflection bridge, and uncompressed frame behavior. All variable-length reads use the caller's validated `protocol.Limits`; later plans add generated codecs, compression, encryption, and managed streams.

**Tech Stack:** Go 1.26.5 from `openserbia/go-flake`, Devbox, Task, the Go standard library, and Java Edition protocol 47 compatibility fixtures from `server`.

## Status and scope

The committed foundations are the starting point:

- `minecraft-protocol` commit `eee7c1c68caf` defines `protocol.Packet`, `protocol.Protocol`, `protocol.Codec`, and `protocol.Limits` at the module root.
- `headless-minecraft` commit `afab923a311a` consumes that module-root API.
- This plan supersedes Tasks 1 and 2 of `2026-08-13-shared-protocol-extraction.md`.
- Task 3 of the shared extraction plan resumes after this plan passes its release gate.

This plan does not move game data, the generator, generated protocol 47 packets, or any server import. It does not add compression, encryption, protocol 775, streams, routers, capture, login, or the `mcproto` command.

## Global constraints

- Work in `/home/ocharnyshevich/pet.projects/go-theft-craft/minecraft-protocol` unless a step names another repository.
- Run Go, formatting, lint, test, and build commands through `devbox run -- task <name>`.
- Leave changes uncommitted unless the user explicitly requests a commit.
- Preserve the existing module-root `protocol.Packet` struct and `protocol.Protocol.NewCodec(Role, Limits) (Codec, error)` signature.
- Reuse `protocol.Limits`. Do not add `wire/java.Limits` or `wire/java.DefaultLimits`.
- Reject an invalid zero-value `protocol.Limits` before reading, allocating, or writing variable-length data.
- Use `io.ReadFull` for fixed or declared lengths and a full-write helper for complete encoded values.
- Bound frame bytes, string bytes, byte-array bytes, and recursive decoding before allocation.
- Preserve protocol 47 byte encoding for all inputs accepted by the current server package.
- Keep the reflection bridge only for compatibility with the existing protocol 47 generator. The current-protocol plan replaces it with generated codecs.
- Do not modify or delete `server/pkg/protocol` in this milestone.
- Do not change `headless-minecraft` source files in this milestone.

---

### Task 1: Make focused Task tests runnable

**Files:**

- Modify: `minecraft-protocol/Taskfile.yml`
- Modify: `headless-minecraft/docs/superpowers/plans/2026-08-13-shared-protocol-extraction.md`

**Interfaces:**

- Consumes: the existing `test` task and Task's `.CLI_ARGS` value.
- Produces: `devbox run -- task test -- <packages or go test flags>`, with `./...` as the default package argument.

- [x] **Step 1: Record the clean baseline**

Run:

```bash
git status --short
devbox run -- task test
```

Expected: the worktree is clean and the existing protocol tests pass.

- [x] **Step 2: Add a Taskfile argument test**

Create a temporary package selection failure by running:

```bash
devbox run -- task test -- ./wire/java
```

Expected: the command still tests `./...` or does not select `./wire/java`, which proves that the current task ignores `.CLI_ARGS`.

- [x] **Step 3: Make the test task accept package arguments**

Change the command in `Taskfile.yml` to:

```yaml
  test:
    desc: Run unit tests with the race detector
    deps: [deps]
    cmds:
      - go test -race -covermode=atomic -coverprofile=coverage.out {{.CLI_ARGS | default "./..."}}
```

Keep the existing race detector. Do not add a second `test:race` task.

- [x] **Step 4: Mark the replaced plan sections**

Add this note below the header of `2026-08-13-shared-protocol-extraction.md`:

```markdown
> [!NOTE]
> Tasks 1 and 2 describe the pre-foundation package layout. The repository
> foundation is complete. Use `2026-08-13-java-1-8-wire-extraction.md` for the
> Java wire milestone, then resume this plan at Task 3.
```

Do not rewrite Tasks 3 through 8 in this milestone.

- [x] **Step 5: Verify focused and default test selection**

Run:

```bash
devbox run -- task test -- ./
devbox run -- task test
```

Expected: both commands pass. The first command tests only the module-root package. The second command tests every package.

- [x] **Step 6: Inspect the checkpoint**

Run:

```bash
git diff --check
git status --short
```

Expected: only `Taskfile.yml` is visible because the local `docs/` tree is ignored. Do not commit.

### Task 2: Add Java VarInt and fixed-field primitives

**Files:**

- Create: `minecraft-protocol/wire/java/errors.go`
- Create: `minecraft-protocol/wire/java/io.go`
- Create: `minecraft-protocol/wire/java/varint.go`
- Create: `minecraft-protocol/wire/java/varint_test.go`
- Create: `minecraft-protocol/wire/java/fields.go`
- Create: `minecraft-protocol/wire/java/fields_test.go`

**Interfaces:**

- Consumes: `io.Reader`, `io.Writer`, and validated `protocol.Limits` for variable-length values.
- Produces: `ErrInvalidLimits`, `ErrVarIntTooLong`, `ErrVarLongTooLong`, `ReadVarInt`, `WriteVarInt`, `PutVarInt`, `VarIntSize`, `ReadVarLong`, `WriteVarLong`, Java position helpers, fixed-width numeric helpers, UUID helpers, bounded string helpers, and bounded byte-array helpers.

- [x] **Step 1: Write VarInt compatibility tests**

Copy the table cases from `server/pkg/protocol/varint_test.go` and add exact encoded-byte assertions for these values:

```go
tests := []struct {
	name  string
	value int32
	want  []byte
}{
	{name: "zero", value: 0, want: []byte{0x00}},
	{name: "one", value: 1, want: []byte{0x01}},
	{name: "127", value: 127, want: []byte{0x7f}},
	{name: "128", value: 128, want: []byte{0x80, 0x01}},
	{name: "maximum", value: math.MaxInt32, want: []byte{0xff, 0xff, 0xff, 0xff, 0x07}},
	{name: "minus one", value: -1, want: []byte{0xff, 0xff, 0xff, 0xff, 0x0f}},
}
```

Test `ReadVarInt` with a reader that returns one byte per call. Assert that six continuation bytes return `ErrVarIntTooLong`. Add equivalent round-trip and eleven-byte rejection tests for VarLong.

- [x] **Step 2: Run the VarInt tests and verify failure**

Run:

```bash
devbox run -- task test -- ./wire/java -run 'Test(VarInt|VarLong)'
```

Expected: compilation fails because `wire/java` has no implementation.

- [x] **Step 3: Implement complete VarInt writes**

Port the encoding behavior from `server/pkg/protocol/varint.go`. Route every public write through an unexported helper:

```go
func writeFull(w io.Writer, data []byte) (int, error) {
	written := 0
	for written < len(data) {
		n, err := w.Write(data[written:])
		written += n
		if err != nil {
			return written, err
		}
		if n == 0 {
			return written, io.ErrShortWrite
		}
	}
	return written, nil
}
```

Return sentinel errors with context so callers can use `errors.Is`.

- [x] **Step 4: Write field and limit tests**

Cover signed fixed-width values, floats, booleans, UUIDs, negative positions, and one-byte readers. Add these variable-length cases:

- A string at `limits.StringBytes()` passes.
- A string one byte over the limit returns a typed size error before allocation.
- A negative string length fails.
- A byte array at `limits.CollectionItems()` passes.
- A byte array one byte over the limit fails before allocation.
- A zero-value `protocol.Limits` returns `ErrInvalidLimits`.
- A writer that accepts one byte per call still receives the full value.
- A writer that returns `(0, nil)` causes `io.ErrShortWrite`.

Construct small test limits through the public API:

```go
limits, err := protocol.NewLimits(
	protocol.MaxStringBytes(4),
	protocol.MaxCollectionItems(4),
)
if err != nil {
	t.Fatal(err)
}
```

- [x] **Step 5: Implement bounded field helpers**

Port the primitive behavior from `server/pkg/protocol/varint.go`. Use these signatures for values that allocate:

```go
func ReadString(r io.Reader, limits protocol.Limits) (string, error)
func WriteString(w io.Writer, limits protocol.Limits, value string) (int, error)
func ReadByteArray(r io.Reader, limits protocol.Limits) ([]byte, error)
func WriteByteArray(w io.Writer, limits protocol.Limits, value []byte) (int, error)
```

Check the encoded byte count against `StringBytes()` and the byte-array count against `CollectionItems()`. Reject invalid limits and negative lengths before calling `make`.

- [x] **Step 6: Run the Java primitive tests**

Run:

```bash
devbox run -- task test -- ./wire/java -run 'Test(VarInt|VarLong|Position|Field|String|ByteArray|UUID)'
```

Expected: all selected tests pass under the race detector.

- [x] **Step 7: Inspect the checkpoint**

Run:

```bash
git diff --check
git status --short
```

Expected: only the Taskfile change and new `wire/java` files appear. Do not commit.

### Task 3: Add bounded uncompressed Java frames

**Files:**

- Create: `minecraft-protocol/wire/java/frame.go`
- Create: `minecraft-protocol/wire/java/frame_test.go`

**Interfaces:**

- Consumes: `protocol.Packet`, `protocol.Limits`, `ReadVarInt`, `WriteVarInt`, and `writeFull`.
- Produces: `ErrFrameTooLarge`, `ReadRawPacket(io.Reader, protocol.Limits) (protocol.Packet, error)`, and `WriteRawPacket(io.Writer, protocol.Limits, protocol.Packet) error`.

- [x] **Step 1: Write frame boundary tests**

Cover these cases:

- A frame with packet ID `0x00` and an empty body round trips.
- A frame with a multi-byte packet ID round trips.
- A reader that returns one byte at a time succeeds.
- A declared length of zero fails.
- A negative declared length fails.
- A declared length of `limits.FrameBytes()+1` returns `ErrFrameTooLarge` before allocation.
- A truncated declared payload returns `io.ErrUnexpectedEOF` through `errors.Is`.
- An overlong packet-ID VarInt returns `ErrVarIntTooLong`.
- A packet whose encoded ID plus payload exceeds the frame limit fails before writing.
- A one-byte writer receives the complete frame.
- A `(0, nil)` writer returns `io.ErrShortWrite`.
- Invalid limits fail before the function touches the reader or writer.

Use an envelope rather than introducing a second packet type:

```go
want := protocol.Packet{
	ID:      0x01,
	Payload: []byte{0xaa, 0xbb},
}
```

- [x] **Step 2: Run the frame tests and verify failure**

Run:

```bash
devbox run -- task test -- ./wire/java -run 'Test(ReadRawPacket|WriteRawPacket)'
```

Expected: compilation fails because `ReadRawPacket` and `WriteRawPacket` do not exist.

- [x] **Step 3: Implement uncompressed framing**

Use these signatures:

```go
func ReadRawPacket(r io.Reader, limits protocol.Limits) (protocol.Packet, error)
func WriteRawPacket(w io.Writer, limits protocol.Limits, packet protocol.Packet) error
```

`ReadRawPacket` sets only `ID` and an owned `Payload`. The caller or stateful codec sets `State`, `Direction`, `Name`, and `Value`. `WriteRawPacket` encodes only `ID` and `Payload`; it ignores the other envelope fields.

Read the declared payload with `io.ReadFull`. Build an outbound frame in memory only after checked integer addition proves that the encoded ID and body fit `limits.FrameBytes()`.

- [x] **Step 4: Run all frame and primitive tests**

Run:

```bash
devbox run -- task test -- ./wire/java
```

Expected: every `wire/java` test passes under the race detector.

- [x] **Step 5: Inspect the checkpoint**

Run:

```bash
git diff --check
git status --short
```

Expected: `frame.go` and `frame_test.go` join the previous milestone files. Do not commit.

### Task 4: Port the protocol 47 reflection bridge

**Files:**

- Create: `minecraft-protocol/wire/java/value.go`
- Create: `minecraft-protocol/wire/java/field_codec.go`
- Create: `minecraft-protocol/wire/java/reflect_codec.go`
- Create: `minecraft-protocol/wire/java/reflect_codec_test.go`

**Interfaces:**

- Consumes: the Java primitive and frame helpers from Tasks 2 and 3.
- Produces: `PacketValue`, `Marshal`, `Unmarshal`, `ReadPacket`, and `WritePacket` for existing protocol 47 structs that use `mc` field tags.

- [x] **Step 1: Define the compatibility interface in tests**

Use a name that cannot collide with `protocol.Packet`:

```go
type PacketValue interface {
	PacketID() int32
}
```

Use representative packet structs from `server/pkg/protocol/marshal_test.go`:

```go
type testPacket struct {
	EntityID int32  `mc:"i32"`
	Name     string `mc:"string"`
	Grounded bool   `mc:"bool"`
}

func (testPacket) PacketID() int32 { return 0x01 }
```

- [x] **Step 2: Write reflection and packet tests**

Port the existing marshal, VarInt-tag, and rest-tag tests. Add cases for:

- A non-struct value passed to `Marshal`.
- A nil or non-pointer value passed to `Unmarshal`.
- An unknown `mc` tag.
- A type mismatch between a tag and its Go field.
- A bounded string and byte array that exceed their selected limits.
- A `rest` field that owns its decoded bytes.
- A mismatched packet ID in `ReadPacket`.
- A short writer in `WritePacket`.

- [x] **Step 3: Run the reflection tests and verify failure**

Run:

```bash
devbox run -- task test -- ./wire/java -run 'Test(Marshal|Unmarshal|ReadPacket|WritePacket)'
```

Expected: compilation fails because the reflection bridge does not exist.

- [x] **Step 4: Implement field dispatch with checked assertions**

Use these internal signatures:

```go
func writeField(w io.Writer, limits protocol.Limits, tag string, value any) error
func readField(r io.Reader, limits protocol.Limits, tag string) (any, error)
```

Replace the server package's unchecked type assertions with checked assertions that return the field name, tag, expected type, and actual type. Keep `rest` last. Reject a struct with a field after `mc:"rest"` because no bytes remain for that field.

- [x] **Step 5: Implement the reflection bridge**

Use these public signatures:

```go
func Marshal(value PacketValue, limits protocol.Limits) ([]byte, error)
func Unmarshal(data []byte, value PacketValue, limits protocol.Limits) error
func ReadPacket(r io.Reader, limits protocol.Limits, value PacketValue) error
func WritePacket(w io.Writer, limits protocol.Limits, value PacketValue) error
```

`WritePacket` marshals the value, constructs `protocol.Packet{ID: value.PacketID(), Payload: data}`, and calls `WriteRawPacket`. `ReadPacket` reads one envelope, verifies the ID, and unmarshals the payload into the supplied value.

- [x] **Step 6: Run the complete Java wire test suite**

Run:

```bash
devbox run -- task test -- ./wire/java
```

Expected: all compatibility, boundary, short-I/O, and limit tests pass under the race detector.

- [x] **Step 7: Inspect the checkpoint**

Run:

```bash
git diff --check
git status --short
```

Expected: the reflection bridge files join the earlier Java wire files. Do not commit.

### Task 5: Prove protocol 47 parity and repository health

**Files:**

- Create: `minecraft-protocol/wire/java/compat_test.go`
- Modify: `minecraft-protocol/README.md`
- Modify: `minecraft-protocol/CHANGELOG.md`

**Interfaces:**

- Consumes: the complete `wire/java` API and the existing server protocol fixtures.
- Produces: a parity test corpus and documented pre-alpha Java wire support.

- [x] **Step 1: Add protocol 47 parity vectors**

Add a table of stable hex vectors captured from the existing server implementation. Include:

- Handshake-like strings and VarInts.
- Negative and maximum VarInts.
- Signed positions at zero and negative coordinates.
- A packet with a `rest` payload.
- An empty-body packet.

Store expected bytes as literals in the test so the new and old implementations cannot agree by sharing code:

```go
tests := []struct {
	name string
	read []byte
	want protocol.Packet
}{
	{
		name: "empty body",
		read: []byte{0x01, 0x00},
		want: protocol.Packet{ID: 0x00, Payload: []byte{}},
	},
}
```

- [x] **Step 2: Run the new package and root tests**

Run:

```bash
devbox run -- task test -- ./ ./wire/java
```

Expected: the module-root and Java wire packages pass under the race detector.

- [x] **Step 3: Run the unchanged server test gate**

From `/home/ocharnyshevich/pet.projects/go-theft-craft/server`, run:

```bash
devbox run -- task test
```

Expected: all server tests pass. The server still uses its local protocol package, so this result proves that extraction work did not disturb the source implementation.

- [x] **Step 4: Document the exact support level**

Update the `minecraft-protocol` README support table to say that uncompressed Java primitives and frames are implemented while the built-in protocol 47 descriptor remains planned. Add an Unreleased changelog entry with the exported `wire/java` API and its limits.

Do not claim support for compression, encryption, login, generated codecs, or a complete server session.

- [x] **Step 5: Run the protocol repository release gate**

From `/home/ocharnyshevich/pet.projects/go-theft-craft/minecraft-protocol`, run:

```bash
devbox run -- task verify
```

Expected: formatting, lint, secret scanning, race-enabled tests, vulnerability scanning, and build all pass.

- [x] **Step 6: Inspect the final scope**

Run:

```bash
git diff --check
git status --short
git diff --stat
```

Expected: changes are limited to `Taskfile.yml`, `wire/java`, `README.md`, and `CHANGELOG.md`. The ignored replacement-plan note remains local. Do not commit.

## Completion criteria

This milestone is complete only when all of these statements are true:

- `wire/java` reproduces accepted protocol 47 primitive and uncompressed-frame bytes.
- Every declared length is checked against validated `protocol.Limits` before allocation.
- Short reads and short writes cannot silently truncate a value or frame.
- The compatibility interface is named `PacketValue`; `protocol.Packet` remains the shared envelope.
- `devbox run -- task test -- ./wire/java` runs only the intended package and passes with the race detector.
- `devbox run -- task verify` passes in `minecraft-protocol`.
- `devbox run -- task test` passes in the unchanged `server` repository.
- The README states the narrow support level without implying that a built-in protocol or session exists.
- No change is committed.

After completion, resume `2026-08-13-shared-protocol-extraction.md` at Task 3, which ports immutable game-data contracts.

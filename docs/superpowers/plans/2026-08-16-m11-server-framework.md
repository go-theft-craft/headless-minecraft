# M11 Server Framework Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `subagent-driven-development` or execute this plan inline one task at a time. Keep every checkbox current.

**Goal:** Turn `server` from an application into a framework: composable pieces with working defaults, wired by the application, without breaking the harness role that M6.1, M9, and M10 depend on.

**Architecture:** `server.New` takes options and returns a server whose every subsystem is reachable through an interface with a default implementation. `cmd/server` becomes `examples/vanilla` and the byte-parity fixtures and pinned Node client lane point at it, so the framework refactor is proven by the same gates the application passed. Plain Go, compile-time checked, no registry and no lifecycle magic — the same shape `minecraft-protocol` already uses.

**Tech Stack:** Go 1.26.6 from `openserbia/go-flake`, Devbox, Task, `minecraft-protocol` v0.2.0, and the existing `pkg/world`, `pkg/world/gen`, and `pkg/world/anvil` packages.

## Global Constraints

- Work in `/home/ocharnyshevich/pet.projects/go-theft-craft/server`.
- Run project commands as `devbox run -- task <name>`.
- Leave changes uncommitted unless explicitly requested.
- **The harness role must never break.** `server` is the test harness proving
  `minecraft-protocol` against real clients and giving `headless-minecraft` and
  `minecraft-simulation` something to connect to. M6.1, M9, and M10 all depend
  on it. A framework refactor that breaks the harness has failed regardless of
  how clean it is.
- The M3 byte-parity fixtures and the pinned Node client lane must stay green
  through every sub-milestone. After M11.1 they point at `examples/vanilla`.
- No change to `minecraft-protocol`. The version boundary lives in `server`.
- No dynamic plugin loading. Go has no usable mechanism and pretending
  otherwise buys a familiar shape without the property that made it worth
  having in Java.
- No rollback. Provenance is audit only.
- No vanilla parity as a goal. The framework ships stubs and defaults, and a
  server wanting vanilla behaviour implements it.
- `examples/` is its own Go module. The library keeps the dependency list it
  has; examples pull whatever they need to be realistic.
- Provenance data holds player UUIDs and names. Local runtime data, never
  committed, default location in `.gitignore`.

---

## Stage M11.1: framework shape

**Dependency:** M6.1, which deletes the last hand-written play packet structs.
Building the version boundary while play still runs on 1.8-shaped structs would
mean building it twice.

**Delivers:** `server.New` and options, `cmd/server` moved to `examples/`, seams
declared, plain resource counters.

### Task 1: The public constructor and options

**Files:**
- Create: `server.go`
- Create: `options.go`
- Create: `options_test.go`
- Create: `doc.go`
- Modify: `internal/server/server.go`

**Interfaces:**
- Consumes: `internal/server.New(cfg *config.Config, log *slog.Logger, store *storage.Storage)`, which exists today and stays as the implementation.
- Produces:

```go
// Package server is the root of the framework.
package server

type Server struct { /* unexported; wraps internal/server.Server */ }

type Option func(*options) error

func New(opts ...Option) (*Server, error)
func (s *Server) Start(ctx context.Context) error
func (s *Server) Close() error

func WithListen(addr string) Option
func WithLogger(log *slog.Logger) Option
func WithStore(store WorldStore) Option
func WithGenerator(gen Generator) Option
func WithCommands(set CommandSet) Option
func WithObserver(o Observer) Option
func WithOnlineMode(enabled bool) Option
func WithCompressionThreshold(threshold int) Option
```

Every option validates its own value and returns an error rather than panicking
or silently clamping, which is how `headless-minecraft`'s client options already
behave. `New` performs no network, no disk, and no key generation: it validates
and constructs, and `Start` does the rest.

- [ ] **Step 1: Write the failing test**

```go
func TestNewRejectsABadListenAddress(t *testing.T) {
    t.Parallel()

    if _, err := server.New(server.WithListen("not-a-host-port")); !errors.Is(err, server.ErrInvalidServer) {
        t.Fatalf("got %v, want ErrInvalidServer", err)
    }
}

func TestNewAppliesDefaultsForEveryUnsetSeam(t *testing.T) {
    t.Parallel()

    // A framework whose zero configuration does not run is a framework nobody
    // starts. Every seam has a working default.
    srv, err := server.New()
    if err != nil {
        t.Fatalf("New: %v", err)
    }
    defer func() { _ = srv.Close() }()
}

func TestNewDoesNoIO(t *testing.T) {
    t.Parallel()

    // Construction must not bind a port, touch the disk, or generate a key.
    // A test that constructs a hundred servers should cost nothing.
    for range 100 {
        if _, err := server.New(); err != nil {
            t.Fatalf("New: %v", err)
        }
    }
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `devbox run -- task test -- .`
Expected: FAIL, `undefined: server.New`.

- [ ] **Step 3: Implement options and the constructor**

```go
var ErrInvalidServer = errors.New("invalid server configuration")

type options struct {
    listen      string
    log         *slog.Logger
    store       WorldStore
    generator   Generator
    commands    CommandSet
    observer    Observer
    onlineMode  bool
    compression int
}

func New(opts ...Option) (*Server, error) {
    o := defaults()
    for i, opt := range opts {
        if opt == nil {
            return nil, fmt.Errorf("%w: option %d is nil", ErrInvalidServer, i)
        }
        if err := opt(o); err != nil {
            return nil, fmt.Errorf("%w: %w", ErrInvalidServer, err)
        }
    }

    return &Server{opts: o}, nil
}
```

Move key generation out of `main` and into `Start`, where the failure is the
server's rather than the application's.

- [ ] **Step 4: Run the tests**

Run: `devbox run -- task test -- .`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server.go options.go options_test.go doc.go internal/
git commit -m "feat(server): add the public constructor and options"
```

### Task 2: Declare the seams

**Files:**
- Create: `seams.go`
- Create: `seams_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: the interfaces the options accept. They are declared in M11.1 and
  implemented across M11.2 through M11.7, so the shape is fixed once and the
  later stages fill it rather than renegotiating it.

```go
// WorldStore persists the vanilla world. Anvil is one adapter, not the store:
// a version-neutral core cannot have a version-specific native format.
type WorldStore interface {
    LoadChunk(ctx context.Context, pos ChunkPos) (*Chunk, error)
    SaveSnapshot(ctx context.Context, snap Snapshot) error
    Close() error
}

// SideStore holds what the vanilla format has no field for. It is written from
// the same snapshot as the world and stamped with the same generation, so a
// mismatched pair is detected at load rather than trusted.
type SideStore interface {
    SaveSnapshot(ctx context.Context, snap Snapshot, gen Generation) error
    Load(ctx context.Context, gen Generation) (Sidecar, error)
    Close() error
}

// Generator produces chunks in the version-neutral model.
type Generator interface {
    Generate(ctx context.Context, pos ChunkPos) (*Chunk, error)
    ID() string
}

// Observer takes typed samples. One interface, not four systems: per-player
// load, per-feature load, chunk timing, CPU, memory, and network are all
// samples.
type Observer interface {
    Sample(ctx context.Context, s Sample)
}

// Command is one command. CommandSet is a set of them, rendered to a brigadier
// tree on protocol 775 and to tab-complete on 47 by the version boundary.
type Command interface {
    Name() string
    Aliases() []string
    Run(ctx context.Context, caller Caller, args Args) error
}

type CommandSet interface {
    Lookup(name string) (Command, bool)
    All() []Command
}
```

**One deliberate departure from the design.** It names this interface `Set`.
Declared in package `server`, that reads as `server.Set` at every call site,
which says nothing about what is in it and collides with the ordinary meaning of
the word in an options API. `CommandSet` costs seven characters and is the name
a reader can guess. If the design's name is preferred, change it here before
Task 2 rather than after M11.7 has rendered it to a brigadier tree.

- [ ] **Step 1: Write the failing test**

```go
func TestEverySeamHasAWorkingDefault(t *testing.T) {
    t.Parallel()

    // The defaults are what makes server.New() with no options run. Each is
    // asserted here so a later stage replacing one cannot quietly remove it.
    var (
        _ server.WorldStore = server.NoStore()
        _ server.Generator  = server.EmptyGenerator()
        _ server.Observer   = server.NoObserver()
        _ server.CommandSet = server.NoCommands()
    )
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `devbox run -- task test -- .`
Expected: FAIL, `undefined: server.NoStore`.

- [ ] **Step 3: Implement the defaults**

`NoStore` keeps chunks in memory and discards them on close. `EmptyGenerator`
returns air. `NoObserver` drops samples. `NoCommands` resolves nothing. Each is
a few lines, and each is what makes `server.New()` with no options a running
server rather than a configuration exercise.

- [ ] **Step 4: Run the tests**

Run: `devbox run -- task test -- .`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add seams.go seams_test.go && git commit -m "feat(server): declare the framework seams"
```

### Task 3: Move `cmd/server` to `examples/vanilla`

**Files:**
- Create: `examples/go.mod`
- Create: `examples/vanilla/main.go`
- Create: `examples/minimal/main.go`
- Create: `examples/flat/main.go`
- Delete: `cmd/server/main.go`
- Modify: `Taskfile.yml`
- Modify: `.github/workflows/*.yml`
- Modify: `interop/node_client_test.go`

**Interfaces:**
- Consumes: `server.New` and every option from Task 1.
- Produces: three examples that compose different sets of framework pieces.
  `minimal` accepts a login into an empty world with no storage. `flat` is
  superflat and in-memory. `vanilla` is today's `cmd/server`, unchanged in
  behaviour.

A framework whose only example is the full server has not shown that its pieces
come apart. That is what `minimal` and `flat` are for, and neither is a
demonstration: both are compiled and run by CI.

- [ ] **Step 1: Create the examples module**

```bash
mkdir -p examples/vanilla examples/minimal examples/flat
cd examples && go mod init github.com/go-theft-craft/server/examples
go mod edit -replace github.com/go-theft-craft/server=../
```

- [ ] **Step 2: Write `minimal`, the smallest thing that runs**

```go
func main() {
    srv, err := server.New(server.WithListen(":25565"))
    if err != nil {
        log.Fatal(err)
    }

    ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
    defer stop()

    if err := srv.Start(ctx); err != nil {
        log.Fatal(err)
    }
}
```

If `minimal` needs more than this to accept a login, a default is missing and
Task 2 is not finished.

- [ ] **Step 3: Move `vanilla` verbatim**

Copy `cmd/server/main.go` to `examples/vanilla/main.go` and rewrite its
construction to use `server.New` with options rather than
`internal/server.New`. Every flag keeps its name and its default: the byte-parity
fixtures invoke this binary and a renamed flag breaks them for no reason.

- [ ] **Step 4: Repoint the harness**

Update the Taskfile, the CI workflows, and `interop/node_client_test.go` to
build and run `examples/vanilla`. Add an `examples` task that lints, tests, and
vets the nested module, and call it from `verify` — `go test ./...` from the
root does not descend into a nested module, so without this the examples rot
silently.

- [ ] **Step 5: Run the harness gates**

Run: `devbox run -- task verify`
Run: `devbox run -- task test:parity`
Run: `devbox run -- task test:interop`
Expected: PASS, with byte-parity fixtures unchanged. If a fixture moved, stop:
the refactor changed behaviour and that is the one outcome M11.1 may not have.

- [ ] **Step 6: Commit**

```bash
git add examples Taskfile.yml .github interop && git rm -r cmd
git commit -m "refactor(examples): move cmd/server to examples/vanilla"
```

### Task 4: Plain resource counters

**Files:**
- Create: `observe/counters.go`
- Create: `observe/counters_test.go`
- Modify: `internal/server/server.go`

**Interfaces:**
- Consumes: the `Observer` and `Sample` types from Task 2.
- Produces: CPU, memory, network, and tick-duration samples.

Per-chunk and per-feature attribution is **not** in M11.1. It is sequenced after
M11.2, because measuring a chunk model that is about to be replaced produces
numbers that expire. The counters below do not expire.

- [ ] **Step 1: Write the failing test**

```go
func TestTheTickLoopSamplesItsOwnDuration(t *testing.T) {
    t.Parallel()

    var got []server.Sample
    observer := server.ObserverFunc(func(_ context.Context, s server.Sample) {
        got = append(got, s)
    })

    srv, err := server.New(server.WithObserver(observer))
    if err != nil {
        t.Fatalf("New: %v", err)
    }
    runTicks(t, srv, 3)

    if len(got) < 3 {
        t.Fatalf("three ticks produced %d samples, want at least 3", len(got))
    }
}

func TestNoObserverCostsNothingPerTick(t *testing.T) {
    t.Parallel()

    // Exit criterion 9: turning observability off returns the server to its
    // M6.1 resource profile. A sample built and then discarded is not off.
    allocs := testing.AllocsPerRun(100, func() {
        sampleTick(server.NoObserver(), time.Millisecond)
    })
    if allocs != 0 {
        t.Errorf("a discarded sample allocated %.0f times per tick, want 0", allocs)
    }
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `devbox run -- task test -- ./observe`
Expected: FAIL.

- [ ] **Step 3: Implement the counters**

Sample tick duration in the existing `tickLoop`, and CPU, memory, and network
on a slower cadence. Guard every sample construction behind a check for the
no-op observer so the off path builds nothing.

- [ ] **Step 4: Run the tests**

Run: `devbox run -- task test -- ./observe`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add observe internal/ && git commit -m "feat(observe): sample tick, CPU, memory, and network"
```

---

## Stages awaiting their own design

The server framework design is explicit that it "does not design the
sub-milestones. Each gets its own focused design and implementation plan, the
way M8 subdivided into M8.1 through M8.8." M11.1 is written out above because
its decisions are settled and its code exists to be moved. The rest are not,
and writing tasks for them now would fix interfaces that their own design pass
is supposed to choose.

| Stage | Covers | Depends on | Write its design after |
| --- | --- | --- | --- |
| M11.2 | Interned block states, per-version adapters, immutable sections | M11.1 | M11.1 lands |
| M11.3 | `WorldStore`, native format research, vanilla Anvil adapter, snapshot saving | M11.2 | M11.2 lands |
| M11.4 | Generation parameters, named world types, version-neutral output | M11.2 | M11.2 lands |
| M11.5 | Item and block identity, the ID index, the audit log and its queries | M11.3 | M11.3 lands |
| M11.6 | The `Observer` interface, per-player, per-feature, per-chunk attribution | M11.2 | M11.2 lands |
| M11.7 | `Command`, `Set`, `vanilla.Stubs()`, brigadier on 775, tab-complete on 47 | M11.1 | M11.1 lands |

M11.6 and M11.7 touch the world model lightly and can run alongside M11.2 if
there is capacity. M11.3, M11.4, and M11.5 cannot.

Three things each of those designs must carry, taken from decisions already
made rather than left to be rediscovered:

- **M11.2 rewrites two packages that currently work.** `pkg/world/gen` is
  `Blocks [4096]uint16` with `blockID<<4 | metadata` written into it, and
  `pkg/world/anvil` reads 1.8 regions. Neither survives interned states
  unchanged, and the byte-parity fixtures are the only thing that will say
  whether the harness survived with them.
- **M11.5's ID index is the duplication detector, not a forensic log.** Any
  write placing an existing item ID in a second location without removing it
  from the first is caught where it happens. The same index answers "where is
  this item now" and, persisted, is the item sidecar. It is also the instrument
  that would settle the unexplained survival block duplication in the M3 session
  findings.
- **M11.3 and M11.5 must keep non-vanilla data beside the vanilla file.** Custom
  NBT tags inside Anvil would work and would be dropped silently by any external
  reader, breaking the chain at the first external touch with no signal. The
  cost of separation is a consistency problem, answered by writing both stores
  from one snapshot with a shared generation stamp and reconciling at load.

## Exit criteria for the track

Copied from the design so this plan can be checked without opening it.

| | Criterion |
| --- | --- |
| 1 | `examples/vanilla` passes every gate `cmd/server` passed, including the byte-parity fixtures and the pinned Node client lane |
| 2 | Three examples exist and each composes a different set of framework pieces |
| 3 | One world model serves protocol 47 and protocol 775 with no version type in the core |
| 4 | A save runs to completion with no measurable pause in the tick |
| 5 | A deliberately duplicated item is detected at the write that duplicates it |
| 6 | Placement, restart, destruction by a mob, drop, and transfer produce one connected chain from one query |
| 7 | A world saved with provenance on is readable by an unmodified vanilla tool, and a world edited by one is reconciled at load with every discrepancy recorded |
| 8 | Every vanilla command name resolves, and every unimplemented one says so |
| 9 | Turning provenance and observability off returns the server to its M6.1 resource profile |

Criteria 1 and 2 are M11.1's, and Task 3 and Task 4 above are what close them.

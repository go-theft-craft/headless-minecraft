# Headless Client and Authentication Implementation Plan

> **Status: complete apart from tasks 3, 4, and 5, 2026-08-18.** The lifecycle,
> the offline provider, and the login exchange shipped as M6.3; the client
> connects, reaches play, publishes session events, and closes once. Those
> boxes are ticked by outcome, checked against this repository on 2026-08-18.
> Three tasks never shipped and stay unticked. `auth` holds `Identity`,
> `Provider`, and `Offline` and nothing else, so pluggable token storage
> (task 3) has no code at all, and the Microsoft device-code half (tasks 4
> and 5) is postponed rather than blocked: it has its own plan at
> [2026-08-15-microsoft-authentication.md](2026-08-15-microsoft-authentication.md),
> whose own boxes are open for the same reason.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Create the native Go headless client, support offline and Microsoft-authenticated Java protocol 775 sessions, and expose a scoped lifecycle with typed and raw events.

**Architecture:** The client composes `minecraft-protocol` rather than hiding it. Authentication, session orchestration, normalized events, and the client adapter live in `headless-minecraft`; wire codecs, generated data, and Java login helpers remain shared.

**Tech Stack:** Go 1.26.5 from `openserbia/go-flake`, Devbox, Task, `minecraft-protocol`, standard library HTTP and crypto, Microsoft device authorization, Xbox Live, XSTS, Minecraft Services, `log/slog`, and `errgroup`.

## Global Constraints

- Complete the current protocol and stream toolkit plan first.
- Work in `/home/ocharnyshevich/pet.projects/go-theft-craft/headless-minecraft`.
- Initialize Git on branch `main`; `docs/` stays ignored.
- Run commands only as `devbox run -- task <name>`.
- Leave changes uncommitted unless explicitly requested.
- Use the generated Java 26.1 protocol 775 bundle by default, validate it against a server running the 26.1.2 game build, and accept injected protocol and client adapters.
- Pass context as the first argument to every blocking public operation.
- Do not store caller contexts in public option or request structs.
- Never print, log, trace, or return access tokens, refresh tokens, authorization headers, shared secrets, or device codes in errors.
- Do not persist authentication unless the caller supplies a token store.
- Do not reconnect automatically.

---

### Task 1: Bootstrap the headless client repository

**Files:**
- Create: `.gitignore`
- Create: `.envrc`
- Create: `go.mod`
- Create: `devbox.json`
- Create: `Taskfile.yml`
- Create: `.golangci.yml`
- Create: `internal/buildcheck/buildcheck_test.go`

**Interfaces:**
- Produces: module `github.com/go-theft-craft/headless-minecraft` and the standard Devbox and Task workflow.

- [x] **Step 1: Initialize Git and add repository files**

Run `git init -b main .`. Keep the existing ignored design and plan files. Add the sibling module during local development:

```go
require github.com/go-theft-craft/minecraft-protocol v0.0.0

replace github.com/go-theft-craft/minecraft-protocol => ../minecraft-protocol
```

- [x] **Step 2: Add Devbox and direnv configuration**

Use the same pinned Go and tooling versions as `minecraft-protocol`. Add:

```bash
#!/bin/bash

eval "$(devbox generate direnv --print-envrc)"
```

- [x] **Step 3: Add standard tasks and expected failing import test**

Add `deps`, `fmt`, `lint`, `test`, `test:race`, `build`, and `verify`. The build check imports future `auth`, `client`, and `event` packages.

- [x] **Step 4: Verify the baseline failure**

Run `devbox run -- task test`.

Expected: compilation fails because the public packages do not exist.

### Task 2: Define authentication identities and offline mode

**Files:**
- Create: `auth/provider.go`
- Create: `auth/identity.go`
- Create: `auth/offline.go`
- Create: `auth/offline_test.go`

**Interfaces:**
- Produces: `auth.Provider`, `auth.Identity`, and `auth.Offline`.

- [x] **Step 1: Write identity and offline UUID tests**

Verify username validation, deterministic UUID generation from `OfflinePlayer:<username>`, no token fields, and context cancellation before work.

- [x] **Step 2: Define the provider contract**

```go
type Provider interface {
	Authenticate(context.Context) (Identity, error)
}

type Identity struct {
	Username    string
	UUID        uuid.UUID
	AccessToken string
	Online      bool
}
```

Keep sensitive fields out of `String`, `GoString`, JSON, and error formatting.

- [x] **Step 3: Implement offline authentication**

Use the Minecraft offline UUID algorithm based on MD5 name UUID bytes and set RFC 4122 version and variant bits correctly.

- [x] **Step 4: Run tests**

Run `devbox run -- task test -- ./auth`.

### Task 3: Implement pluggable token storage

**Files:**
- Create: `auth/token.go`
- Create: `auth/store.go`
- Create: `auth/memory_store.go`
- Create: `auth/store_test.go`

**Interfaces:**
- Produces: `auth.TokenStore`, `auth.StoredToken`, and concurrency-safe `auth.MemoryStore`.

- [ ] **Step 1: Write store contract tests**

Test missing keys, save and load, overwrite, delete, cancellation, defensive copies, and concurrent access under the race detector.

- [ ] **Step 2: Define the minimal interface**

```go
type TokenStore interface {
	Load(context.Context, string) (StoredToken, error)
	Save(context.Context, string, StoredToken) error
	Delete(context.Context, string) error
}
```

Use `ErrTokenNotFound` for a missing key. Do not provide a disk store in v1.

- [ ] **Step 3: Implement and test memory storage**

Run `devbox run -- task test:race -- ./auth`.

### Task 4: Implement Microsoft device authorization and token refresh

**Files:**
- Create: `auth/microsoft/options.go`
- Create: `auth/microsoft/device.go`
- Create: `auth/microsoft/oauth.go`
- Create: `auth/microsoft/errors.go`
- Create: `auth/microsoft/oauth_test.go`

**Interfaces:**
- Produces: `microsoft.New(Options) auth.Provider` and a caller callback for device instructions.

- [ ] **Step 1: Add HTTP transcript tests**

Use `httptest.Server` and injected endpoint URLs. Cover device-code success, `authorization_pending`, `slow_down`, expiry, denial, context cancellation, refresh success, invalid refresh fallback, malformed JSON, and redacted errors.

- [ ] **Step 2: Define options without embedded secrets**

```go
type Options struct {
	ClientID     string
	Store        auth.TokenStore
	StoreKey     string
	HTTPClient   *http.Client
	OnDeviceCode func(context.Context, DeviceCode) error
	Endpoints    Endpoints
}
```

Default endpoints are Microsoft consumer device authorization and token endpoints. Default scope is `XboxLive.signin offline_access`. Require `ClientID`; do not ship another application's client ID.

- [ ] **Step 3: Implement device polling and refresh**

Honor server polling intervals, increase them on `slow_down`, stop at expiry, and check context during every wait and HTTP request. Save refresh tokens only through the supplied store.

- [ ] **Step 4: Run focused tests**

Run `devbox run -- task test -- ./auth/microsoft`.

### Task 5: Implement Xbox and Minecraft Services exchanges

**Files:**
- Create: `auth/microsoft/xbox.go`
- Create: `auth/microsoft/minecraft.go`
- Create: `auth/microsoft/provider.go`
- Create: `auth/microsoft/provider_test.go`

**Interfaces:**
- Consumes: Microsoft OAuth token.
- Produces: verified Minecraft `auth.Identity` with profile UUID, username, and access token.

- [ ] **Step 1: Add full exchange transcript tests**

Cover Xbox user authentication, XSTS authorization, Minecraft login, entitlement lookup, profile lookup, child-account XSTS errors, no-game entitlement, absent profile, token refresh reuse, and redaction.

- [ ] **Step 2: Implement the exchange chain**

Use these default HTTPS endpoints:

```text
https://user.auth.xboxlive.com/user/authenticate
https://xsts.auth.xboxlive.com/xsts/authorize
https://api.minecraftservices.com/authentication/login_with_xbox
https://api.minecraftservices.com/entitlements/mcstore
https://api.minecraftservices.com/minecraft/profile
```

Validate HTTP status before decoding success payloads. Limit response bodies. Never disable TLS verification.

- [ ] **Step 3: Return the online identity**

Set `Online` true only after entitlement and profile checks pass. Parse the compact profile UUID into `uuid.UUID`.

- [ ] **Step 4: Run authentication tests and race tests**

Run `devbox run -- task test:race -- ./auth/...`.

### Task 6: Define events and bounded subscriptions

**Files:**
- Create: `event/event.go`
- Create: `event/types.go`
- Create: `event/hub.go`
- Create: `event/subscription.go`
- Create: `event/next.go`
- Create: `event/hub_test.go`

**Interfaces:**
- Produces: `event.Hub`, `Subscription`, `Publish`, `Subscribe`, and package-level generic `event.Next[T]`.

- [x] **Step 1: Write ordering and overflow tests**

Test publication order, filtering by concrete type, cancellation, explicit unsubscribe, immutable payload expectations, slow-subscriber closure with `ErrSubscriptionOverflow`, and hub shutdown.

- [x] **Step 2: Implement bounded subscriptions**

Publishing must not invoke user callbacks. Each subscription has a fixed buffer. An overflow records the error and closes only that subscription.

- [x] **Step 3: Add the generic helper**

```go
func Next[T Event](ctx context.Context, sub *Subscription) (T, error)
```

Implement this as a package-level function while Go 1.27 generic methods remain unavailable in the stable toolchain.

- [x] **Step 4: Run race tests**

Run `devbox run -- task test:race -- ./event`.

### Task 7: Define injectable client protocol adapters

**Files:**
- Create: `client/adapter.go`
- Create: `client/options.go`
- Create: `client/errors.go`
- Create: `internal/adapter/java/current.go`
- Create: `internal/adapter/java/current_test.go`

**Interfaces:**
- Produces: `client.Adapter`, `client.Result`, `client.Reducer`, advanced raw `client.WithProtocol`, `client.WithReducer`, and the default Java 26.1 protocol 775 adapter. The later constructed-components plan wraps these in complete `client.WithVersion` conformance profiles before enabling high-level gameplay APIs.

- [x] **Step 1: Test option validation**

Require address and authentication. Verify default current protocol, paired custom protocol and adapter, injected dialer, limits, logger, and rejection of a Java adapter paired with a custom edition. Mark direct `WithProtocol` construction as raw lifecycle and packet access only; it must not imply vanilla-compatible gameplay semantics.

- [x] **Step 2: Define the adapter contract**

```go
type Adapter interface {
	ProtocolID() string
	Handle(context.Context, protocol.Packet) (Result, error)
}

type Result struct {
	Events     []event.Event
	Replies    []protocol.Packet
	Ready      bool
	Disconnect error
}

type Reducer interface {
	Reduce(context.Context, protocol.Packet) ([]event.Event, error)
}
```

Keep wire encoding in `minecraft-protocol`. The adapter handles connection-control packets. Optional reducers observe packets in registration order and produce normalized state events without owning the network stream.

- [x] **Step 3: Implement the current adapter skeleton**

Handle login-ready, configuration-ready, play-ready, keepalive, disconnect, system chat, and raw packet events. Return `ErrUnsupportedPacket` only for packets required to maintain the connection. Optional unknown packets still publish raw events. Run registered reducers after connection-control handling and before publishing the result's events.

- [x] **Step 4: Run adapter tests**

Run `devbox run -- task test -- ./client ./internal/adapter/java`.

### Task 8: Implement the scoped client lifecycle

**Files:**
- Create: `client/client.go`
- Create: `client/run.go`
- Create: `client/send.go`
- Create: `client/state.go`
- Create: `internal/session/session.go`
- Create: `internal/session/session_test.go`
- Create: `mctest/server.go`

**Interfaces:**
- Produces: `client.New`, `(*Client).Run`, `WaitReady`, `Send`, `Events`, `Snapshot`, and idempotent `Close`.

- [x] **Step 1: Write lifecycle transcript tests**

Test offline login to play, online encryption and session join callback, configuration completion, keepalive priority, raw events, callback return cleanup, context cancellation, server disconnect, partial write, and double close.

- [x] **Step 2: Implement gotd-style scoped use**

```go
bot, err := client.New(options...)
if err != nil {
	return err
}

err = bot.Run(ctx, func(ctx context.Context) error {
	return bot.WaitReady(ctx)
})
```

The client is usable only inside `Run`. Returning from the callback closes the session and waits for owned goroutines. Do not reconnect.

- [x] **Step 3: Compose shared login and stream helpers**

Authenticate before dialing. Use `java/login` for handshake through configuration, then start the shared managed stream for play. For online mode, call the Mojang session join endpoint through an injected, tested HTTP client before completing encrypted login.

- [x] **Step 4: Run lifecycle race tests**

Run `devbox run -- task test:race -- ./client ./internal/session ./mctest`.

### Task 9: Add examples, public README, and release verification

**Files:**
- Create: `README.md`
- Create: `examples/offline/main.go`
- Create: `examples/microsoft/main.go`
- Create: `examples/raw/main.go`
- Modify: `Taskfile.yml`

**Interfaces:**
- Produces: runnable examples for high-level lifecycle, Microsoft device flow, and unrestricted raw packet access.

- [x] **Step 1: Add compile tests for examples**

Make `task build` build all example packages. Examples read address, username, Microsoft client ID, and token-store choice from flags. They never contain credentials.

- [x] **Step 2: Document the three usage levels**

README sections show raw codec use in `minecraft-protocol`, managed stream use, and full headless client use. Use `devbox run -- task` in all development commands.

- [x] **Step 3: Run the full verification gate**

Run `devbox run -- task verify` in `minecraft-protocol` and `headless-minecraft`.

Expected: generation, source validation, formatting, lint, tests, race tests, and builds pass.

- [x] **Step 4: Inspect final scope**

Run `git status --short` and `git diff --check` in both repositories. Confirm that no token value can be formatted through public identity or error types. Do not commit.

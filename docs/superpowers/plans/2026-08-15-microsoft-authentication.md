# Microsoft Authentication Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a Microsoft device-code authentication provider behind the `auth.Provider` seam M6.3 defines, so a headless client can log in to an online-mode server with a real account.

**Architecture:** Five HTTP exchanges in a fixed chain — device-code start and poll, Xbox Live user token, XSTS token, Minecraft-services login, and profile lookup — behind one `Provider`. The chain is a sequence of small, individually testable steps over one injected `*http.Client`, so every boundary is mocked with `httptest` and no test reaches the network. Session-server join, the only call made after the client is connected, is the `Authenticator` the identity carries.

**Tech Stack:** Go 1.26.6 via `openserbia/go-flake`, Devbox, Task, standard library only. No OAuth library.

## Global Constraints

- Work in `/home/ocharnyshevich/pet.projects/go-theft-craft/headless-minecraft`.
- Run every command as `devbox run -- task <name>`. Never call `go` directly.
- The module depends on `minecraft-protocol` and nothing else. **Do not add an OAuth or HTTP library.** `net/http` and `encoding/json` cover every exchange here.
- **No secret may appear in an error, a log line, an event, or a test fixture.** Not the access token, not the refresh token, not the device code, not the XSTS user hash. Errors name the exchange that failed and its HTTP status, never its body. `task secrets` runs `gitleaks` and must stay green.
- Use the system trust store. Never set `InsecureSkipVerify`, and never accept a custom root outside a test's own `httptest` server.
- Every exchange takes a `context.Context` and honours it, including the poll loop.
- The library never prints. A device-code flow needs a user to visit a URL, and the application decides how to show it — through a callback, not `fmt.Println`.
- No test may make a real network request. The only exception is one manual, opt-in test gated behind an environment variable, which CI never sets.
- Never add the `Co-Authored-By` or `Claude-Session` trailer to a commit message.
- Run `devbox run -- task precommit` before every commit.

## Dependencies

M6.3's `auth` package, for the `Provider` and `Identity` types this plan
implements a second time. Nothing else. M6.4 is independent of M4, M5, M7, and
the server and proxy work, and it blocks nothing: offline mode covers every
test server and the whole M7 world-state effort.

## The exchange chain

Each arrow is one HTTP request. A failure at any step must name that step.

```text
1. device code    POST login.microsoftonline.com/consumers/oauth2/v2.0/devicecode
                  → user_code, verification_uri, device_code, interval, expires_in
                  ↳ the application shows the code and URL to a person

2. poll           POST login.microsoftonline.com/consumers/oauth2/v2.0/token
                  grant_type=urn:ietf:params:oauth:grant-type:device_code
                  → authorization_pending until the person finishes, then
                    access_token, refresh_token, expires_in

3. Xbox Live      POST user.auth.xboxlive.com/user/authenticate
                  RpsTicket "d=" + the MSA access token
                  → Token, DisplayClaims.xui[0].uhs

4. XSTS           POST xsts.auth.xboxlive.com/xsts/authorize
                  → Token, and the same user hash, which must match step 3's

5. Minecraft      POST api.minecraftservices.com/authentication/login_with_xbox
                  identityToken "XBL3.0 x=<uhs>;<xsts token>"
                  → access_token, expires_in

6. profile        GET api.minecraftservices.com/minecraft/profile
                  → id, name
```

Steps 3 and 4 return XSTS error codes that mean specific, actionable things and
must not collapse into "authentication failed":

| Code | Meaning |
| --- | --- |
| `2148916233` | The account has no Xbox profile |
| `2148916235` | Xbox Live is unavailable in the account's region |
| `2148916236`, `2148916237` | The account needs adult verification |
| `2148916238` | The account is a child and needs to be added to a family |

A person who sees "authentication failed" for any of these has no idea what to
do; a person who sees the real reason does.

## File Structure

**New files:**

| File | Responsibility |
| --- | --- |
| `auth/microsoft/provider.go` | `Provider`, options, the chain |
| `auth/microsoft/devicecode.go` | Steps 1 and 2, including the poll loop |
| `auth/microsoft/xbox.go` | Steps 3 and 4, and XSTS error codes |
| `auth/microsoft/minecraft.go` | Steps 5 and 6 |
| `auth/microsoft/store.go` | `TokenStore`, `MemoryStore`, refresh |
| `auth/microsoft/join.go` | The `login.Authenticator` that joins the session server |
| `auth/microsoft/*_test.go` | One test file per exchange, all over `httptest` |
| `auth/microsoft/manual_test.go` | The one opt-in real-account test |

**Modified:**

| File | Change |
| --- | --- |
| `README.md`, `CHANGELOG.md` | Documentation |
| `../headless-minecraft/MASTER_PLAN.md` | Milestone records |

---

### Task 1: The token store and its refresh rule

Build the store first: every later exchange either reads from it or writes to
it, and the refresh path is what decides whether a second run needs a person at
all.

**Files:**
- Create: `auth/microsoft/store.go`, `auth/microsoft/store_test.go`

**Interfaces:**
- Produces: `Tokens`, `TokenStore`, `MemoryStore`, `(Tokens).NeedsRefresh(now time.Time) bool`, `ErrNoStoredTokens`.

- [ ] **Step 1: Write the failing test**

```go
package microsoft_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/go-theft-craft/headless-minecraft/auth/microsoft"
)

func TestTokensNeedRefreshBeforeTheyActuallyExpire(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)

	// A token expiring in four minutes must be refreshed now: a login that
	// starts inside the window would otherwise expire mid-handshake.
	soon := microsoft.Tokens{ExpiresAt: now.Add(4 * time.Minute)}
	if !soon.NeedsRefresh(now) {
		t.Error("a token expiring in four minutes was not marked for refresh")
	}

	later := microsoft.Tokens{ExpiresAt: now.Add(30 * time.Minute)}
	if later.NeedsRefresh(now) {
		t.Error("a token expiring in thirty minutes was marked for refresh")
	}

	expired := microsoft.Tokens{ExpiresAt: now.Add(-time.Second)}
	if !expired.NeedsRefresh(now) {
		t.Error("an expired token was not marked for refresh")
	}
}

func TestZeroTokensAlwaysNeedRefresh(t *testing.T) {
	if !(microsoft.Tokens{}).NeedsRefresh(time.Now()) {
		t.Error("zero tokens were not marked for refresh")
	}
}

func TestMemoryStoreRoundTrips(t *testing.T) {
	ctx := context.Background()
	store := microsoft.NewMemoryStore()

	if _, err := store.Load(ctx); !errors.Is(err, microsoft.ErrNoStoredTokens) {
		t.Fatalf("empty store returned %v, want ErrNoStoredTokens", err)
	}

	want := microsoft.Tokens{
		AccessToken:  "a",
		RefreshToken: "r",
		ExpiresAt:    time.Now().Add(time.Hour),
	}
	if err := store.Save(ctx, want); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := store.Load(ctx)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.AccessToken != want.AccessToken || got.RefreshToken != want.RefreshToken {
		t.Error("stored tokens did not round-trip")
	}
}

func TestTokensDoNotPrintTheirSecrets(t *testing.T) {
	tokens := microsoft.Tokens{AccessToken: "SECRET-ACCESS", RefreshToken: "SECRET-REFRESH"}

	formatted := fmt.Sprintf("%v %+v %s", tokens, tokens, tokens)
	for _, secret := range []string{"SECRET-ACCESS", "SECRET-REFRESH"} {
		if strings.Contains(formatted, secret) {
			t.Errorf("formatting Tokens disclosed %q", secret)
		}
	}
}
```

Add `"fmt"` and `"strings"` to the imports.

- [ ] **Step 2: Run and verify failure**

```bash
devbox run -- task test -- ./auth/microsoft
```

Expected: FAIL, package does not exist.

- [ ] **Step 3: Implement**

```go
// Package microsoft authenticates a Minecraft account through Microsoft's
// device-code flow.
//
// The flow is five HTTP exchanges in a fixed order, and this package makes
// each one separately testable. It holds no opinion about how a person sees
// their device code: the application supplies a callback, because a library
// that prints to stdout is a library that cannot be used from a service.
//
// No value in this package formats its secrets. Tokens implements Stringer
// and GoStringer to make that structural rather than a rule people remember.
package microsoft

import (
	"context"
	"errors"
	"sync"
	"time"
)

// refreshWindow is how long before expiry a token is considered stale.
//
// A token that expires during a login is worse than one refreshed slightly
// early, and the whole login sequence is well under five minutes.
const refreshWindow = 5 * time.Minute

// ErrNoStoredTokens reports a store with nothing in it. It is not a failure:
// it is how a first run learns it needs a person.
var ErrNoStoredTokens = errors.New("no stored tokens")

// Tokens is one account's Microsoft credentials.
type Tokens struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
}

// NeedsRefresh reports whether these tokens are expired or close enough to
// expiry that a login started now might outlive them.
func (t Tokens) NeedsRefresh(now time.Time) bool {
	return t.ExpiresAt.IsZero() || !now.Add(refreshWindow).Before(t.ExpiresAt)
}

// String hides the secrets. Tokens end up in %v by accident — in a log line,
// a wrapped error, a test failure — and this is the one place that can stop
// it for all of them.
func (t Tokens) String() string { return "microsoft.Tokens{redacted}" }

// GoString hides the secrets under %#v as well.
func (t Tokens) GoString() string { return "microsoft.Tokens{redacted}" }

// TokenStore persists tokens between runs.
//
// The library does not persist anything unless the application supplies a
// store: writing a refresh token to disk is a decision with consequences the
// application owns.
type TokenStore interface {
	Load(ctx context.Context) (Tokens, error)
	Save(ctx context.Context, tokens Tokens) error
}

// MemoryStore keeps tokens for the life of the process. It is the default,
// so an application that supplies no store still refreshes within one run.
type MemoryStore struct {
	mu     sync.Mutex
	tokens Tokens
	set    bool
}

// NewMemoryStore returns an empty in-process store.
func NewMemoryStore() *MemoryStore { return &MemoryStore{} }

// Load implements TokenStore.
func (s *MemoryStore) Load(context.Context) (Tokens, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.set {
		return Tokens{}, ErrNoStoredTokens
	}

	return s.tokens, nil
}

// Save implements TokenStore.
func (s *MemoryStore) Save(_ context.Context, tokens Tokens) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.tokens = tokens
	s.set = true

	return nil
}
```

- [ ] **Step 4: Run and verify it passes**

```bash
devbox run -- task test -- ./auth/microsoft
```

Expected: PASS, all four tests.

- [ ] **Step 5: Commit**

```bash
git add auth/microsoft/store.go auth/microsoft/store_test.go
git commit -m "feat(auth): add the Microsoft token store and refresh rule"
```

### Task 2: The device-code exchange and poll loop

**Files:**
- Create: `auth/microsoft/devicecode.go`, `auth/microsoft/devicecode_test.go`

**Interfaces:**
- Produces: `DeviceCode`, `DeviceCodeCallback`, `requestDeviceCode`, `pollForTokens`, `refreshTokens`, `ErrDeviceCodeExpired`, `ErrAuthorizationDeclined`.

- [ ] **Step 1: Write the failing test**

```go
func TestPollReturnsTokensOnceTheUserApproves(t *testing.T) {
	// The endpoint answers authorization_pending twice, then succeeds. The
	// loop must keep going through the pending answers and stop on the
	// first real one.
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		if calls < 3 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":"authorization_pending"}`)

			return
		}
		_, _ = io.WriteString(w,
			`{"access_token":"at","refresh_token":"rt","expires_in":3600}`)
	}))
	defer server.Close()

	client := newTestClient(server)
	tokens, err := client.pollForTokens(context.Background(), DeviceCode{
		DeviceCode: "dc",
		Interval:   time.Millisecond,
		ExpiresAt:  time.Now().Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("pollForTokens: %v", err)
	}
	if tokens.AccessToken != "at" || tokens.RefreshToken != "rt" {
		t.Error("poll did not return the tokens the endpoint issued")
	}
	if calls != 3 {
		t.Errorf("made %d calls, want 3", calls)
	}
}

func TestPollHonoursSlowDown(t *testing.T) {
	// slow_down means the interval must increase, not that the attempt
	// failed. Assert the loop widened its interval rather than hammering.
	...
}

func TestPollStopsWhenTheUserDeclines(t *testing.T) {
	server := jsonServer(t, http.StatusBadRequest, `{"error":"authorization_declined"}`)
	defer server.Close()

	_, err := newTestClient(server).pollForTokens(context.Background(), testDeviceCode())
	if !errors.Is(err, ErrAuthorizationDeclined) {
		t.Fatalf("got %v, want ErrAuthorizationDeclined", err)
	}
}

func TestPollStopsWhenTheCodeExpires(t *testing.T) {
	server := jsonServer(t, http.StatusBadRequest, `{"error":"expired_token"}`)
	defer server.Close()

	_, err := newTestClient(server).pollForTokens(context.Background(), testDeviceCode())
	if !errors.Is(err, ErrDeviceCodeExpired) {
		t.Fatalf("got %v, want ErrDeviceCodeExpired", err)
	}
}

func TestPollStopsAtTheLocalDeadlineWithoutCallingAgain(t *testing.T) {
	// expires_in has already passed, so the loop must not make a request at
	// all: the code cannot work and the endpoint should not be asked.
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		calls++
	}))
	defer server.Close()

	_, err := newTestClient(server).pollForTokens(context.Background(), DeviceCode{
		DeviceCode: "dc",
		Interval:   time.Millisecond,
		ExpiresAt:  time.Now().Add(-time.Second),
	})
	if !errors.Is(err, ErrDeviceCodeExpired) {
		t.Fatalf("got %v, want ErrDeviceCodeExpired", err)
	}
	if calls != 0 {
		t.Errorf("made %d requests against an expired code, want 0", calls)
	}
}

func TestPollHonoursCancellation(t *testing.T) {
	server := jsonServer(t, http.StatusBadRequest, `{"error":"authorization_pending"}`)
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := newTestClient(server).pollForTokens(ctx, testDeviceCode())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
}

func TestErrorsDoNotIncludeTheResponseBody(t *testing.T) {
	// A failing exchange must name the step and the status, never the body,
	// which can carry a token.
	server := jsonServer(t, http.StatusInternalServerError,
		`{"access_token":"LEAKED-TOKEN"}`)
	defer server.Close()

	_, err := newTestClient(server).pollForTokens(context.Background(), testDeviceCode())
	if err == nil {
		t.Fatal("a 500 was not an error")
	}
	if strings.Contains(err.Error(), "LEAKED-TOKEN") {
		t.Errorf("error disclosed the response body: %v", err)
	}
}
```

Write `TestPollHonoursSlowDown` in full: use a handler that answers
`{"error":"slow_down"}` once and then succeeds, record the times of the two
requests, and assert the gap exceeded the initial interval. Do not leave the
`...` in the committed test.

Write the helpers `newTestClient`, `jsonServer`, and `testDeviceCode` in the
same file. `newTestClient` builds the provider's HTTP client with every
endpoint pointed at the test server, which is why the endpoints must be fields
rather than constants.

- [ ] **Step 2: Run and verify failure**

```bash
devbox run -- task test -- ./auth/microsoft
```

- [ ] **Step 3: Implement**

`DeviceCode` carries `UserCode`, `VerificationURI`, `DeviceCode`, `Interval`,
and `ExpiresAt`. Convert the endpoint's `interval` and `expires_in` seconds to
a `time.Duration` and an absolute deadline at parse time, so the loop compares
against a clock rather than counting.

The loop:

- returns `ErrDeviceCodeExpired` before its first request if the deadline has
  already passed;
- sleeps `interval` between attempts using `select` on `time.After` and
  `ctx.Done()`, never `time.Sleep`;
- treats `authorization_pending` as continue, `slow_down` as continue with the
  interval increased by five seconds, `authorization_declined` and
  `expired_token` as terminal named errors, and any other error code as a
  terminal error naming the code;
- never includes a response body in an error.

`refreshTokens` posts `grant_type=refresh_token` to the same endpoint and
returns the same `Tokens`. Microsoft may or may not return a new refresh
token; when it does not, carry the old one forward rather than storing an
empty string.

- [ ] **Step 4: Run and verify it passes**

- [ ] **Step 5: Commit**

```bash
git add auth/microsoft/devicecode.go auth/microsoft/devicecode_test.go
git commit -m "feat(auth): add the Microsoft device-code exchange"
```

### Task 3: The Xbox Live and XSTS exchanges

**Files:**
- Create: `auth/microsoft/xbox.go`, `auth/microsoft/xbox_test.go`

**Interfaces:**
- Produces: `authenticateXboxLive`, `authorizeXSTS`, `XSTSError` with `Code` and a decoded reason, `ErrNoXboxAccount`, `ErrXboxRegionUnavailable`, `ErrAdultVerificationRequired`, `ErrChildAccount`.

- [ ] **Step 1: Write the failing test**

Table-drive the XSTS error codes, because that mapping is the whole point of
this task:

```go
func TestXSTSErrorCodesDecodeToActionableErrors(t *testing.T) {
	cases := []struct {
		code string
		want error
	}{
		{"2148916233", ErrNoXboxAccount},
		{"2148916235", ErrXboxRegionUnavailable},
		{"2148916236", ErrAdultVerificationRequired},
		{"2148916237", ErrAdultVerificationRequired},
		{"2148916238", ErrChildAccount},
	}

	for _, tc := range cases {
		server := jsonServer(t, http.StatusUnauthorized,
			`{"XErr":`+tc.code+`,"Message":"","Redirect":""}`)

		_, _, err := newTestClient(server).authorizeXSTS(context.Background(), "xbl-token")
		if !errors.Is(err, tc.want) {
			t.Errorf("code %s produced %v, want %v", tc.code, err, tc.want)
		}
		server.Close()
	}
}

func TestUnknownXSTSCodeIsAnErrorNamingTheCode(t *testing.T) {
	server := jsonServer(t, http.StatusUnauthorized, `{"XErr":9999999999}`)
	defer server.Close()

	_, _, err := newTestClient(server).authorizeXSTS(context.Background(), "xbl-token")
	if err == nil || !strings.Contains(err.Error(), "9999999999") {
		t.Fatalf("unknown code produced %v, which does not name it", err)
	}
}

func TestXSTSUserHashMustMatchTheXboxLiveOne(t *testing.T) {
	// A mismatch means the two tokens describe different accounts, which
	// would produce an identityToken that authenticates as nobody.
	...
}

func TestXboxLiveReturnsTokenAndUserHash(t *testing.T) { ... }

func TestXboxErrorsDoNotDiscloseTheRpsTicket(t *testing.T) { ... }
```

Write the three sketched bodies in full following the first two's shape. The
last one posts a known access token, forces a failure, and asserts the token
does not appear in the error.

- [ ] **Step 2: Run and verify failure**

- [ ] **Step 3: Implement**

Both exchanges post JSON and read `Token` plus `DisplayClaims.xui[0].uhs`.
Return the token and the user hash separately, and have the caller compare the
two hashes rather than trusting either.

`XSTSError` carries the numeric code so a caller can match on something stable,
and wraps the named error so `errors.Is` works. The named errors say what the
person must do, not what the API returned.

- [ ] **Step 4: Run and verify it passes**

- [ ] **Step 5: Commit**

```bash
git add auth/microsoft/xbox.go auth/microsoft/xbox_test.go
git commit -m "feat(auth): add the Xbox Live and XSTS exchanges"
```

### Task 4: The Minecraft services exchanges

**Files:**
- Create: `auth/microsoft/minecraft.go`, `auth/microsoft/minecraft_test.go`

**Interfaces:**
- Produces: `loginWithXbox`, `fetchProfile`, `ErrNoMinecraftEntitlement`.

- [ ] **Step 1: Write the failing test**

```go
func TestLoginWithXboxBuildsTheIdentityToken(t *testing.T) {
	// The identityToken format is exact. A wrong separator authenticates as
	// nobody and the failure looks like a rejected account.
	var body struct{ IdentityToken string }
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"mc","expires_in":86400}`)
	}))
	defer server.Close()

	_, err := newTestClient(server).loginWithXbox(context.Background(), "USERHASH", "XSTSTOKEN")
	if err != nil {
		t.Fatalf("loginWithXbox: %v", err)
	}
	if want := "XBL3.0 x=USERHASH;XSTSTOKEN"; body.IdentityToken != want {
		t.Errorf("identity token is %q, want %q", body.IdentityToken, want)
	}
}

func TestProfileParsesTheUndashedUUID(t *testing.T) {
	// Minecraft services returns the UUID without dashes; java.ParseUUID
	// must accept whatever this produces, because the login packet needs it.
	server := jsonServer(t, http.StatusOK,
		`{"id":"069a79f444e94726a5befca90e38aaf5","name":"Notch"}`)
	defer server.Close()

	profile, err := newTestClient(server).fetchProfile(context.Background(), "mc-token")
	if err != nil {
		t.Fatalf("fetchProfile: %v", err)
	}
	if profile.Name.String() != "Notch" {
		t.Errorf("name is %q, want Notch", profile.Name)
	}
	if got := profile.UUID.String(); got != "069a79f4-44e9-4726-a5be-fca90e38aaf5" {
		t.Errorf("UUID is %q, want the dashed form", got)
	}
}

func TestMissingProfileMeansNoEntitlement(t *testing.T) {
	// A 404 here means the account does not own Minecraft, which is a
	// different problem from a failed login and must say so.
	server := jsonServer(t, http.StatusNotFound, `{}`)
	defer server.Close()

	_, err := newTestClient(server).fetchProfile(context.Background(), "mc-token")
	if !errors.Is(err, ErrNoMinecraftEntitlement) {
		t.Fatalf("got %v, want ErrNoMinecraftEntitlement", err)
	}
}

func TestProfileRejectsAnInvalidUsername(t *testing.T) {
	server := jsonServer(t, http.StatusOK, `{"id":"069a79f444e94726a5befca90e38aaf5","name":""}`)
	defer server.Close()

	if _, err := newTestClient(server).fetchProfile(context.Background(), "mc-token"); err == nil {
		t.Fatal("an empty username was accepted")
	}
}
```

- [ ] **Step 2: Run and verify failure**

- [ ] **Step 3: Implement**

`fetchProfile` returns a `login.Profile`, so it parses through
`java.ParseUsername` and `java.ParseUUID` rather than carrying raw strings. If
`ParseUUID` does not accept the undashed form Minecraft services returns,
insert the dashes here rather than changing the shared parser — read
`../minecraft-protocol/wire/java/identity.go:22` before deciding which.

- [ ] **Step 4: Run and verify it passes**

- [ ] **Step 5: Commit**

```bash
git add auth/microsoft/minecraft.go auth/microsoft/minecraft_test.go
git commit -m "feat(auth): add the Minecraft services login and profile lookup"
```

### Task 5: The session-server join authenticator

Everything so far runs before the client dials. This runs during login, when
the server sends its encryption request.

**Files:**
- Create: `auth/microsoft/join.go`, `auth/microsoft/join_test.go`

**Interfaces:**
- Produces: a `login.Authenticator` whose `Join` posts to the session server.

- [ ] **Step 1: Write the failing test**

```go
func TestJoinPostsTheServerHash(t *testing.T) {
	var body struct {
		AccessToken     string `json:"accessToken"`
		SelectedProfile string `json:"selectedProfile"`
		ServerID        string `json:"serverId"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	a := newTestAuthenticator(server, "mc-token", testProfile(t))
	hash := computeTestHash(t)

	if err := a.Join(context.Background(), hash); err != nil {
		t.Fatalf("Join: %v", err)
	}
	if body.ServerID != hash.String() {
		t.Errorf("posted server ID %q, want %q", body.ServerID, hash.String())
	}
	if strings.Contains(body.SelectedProfile, "-") {
		t.Error("selectedProfile carries dashes; the session server wants the undashed form")
	}
}

func TestJoinReportsRejection(t *testing.T) {
	server := jsonServer(t, http.StatusForbidden, `{"error":"ForbiddenOperationException"}`)
	defer server.Close()

	err := newTestAuthenticator(server, "mc-token", testProfile(t)).Join(
		context.Background(), computeTestHash(t))
	if err == nil {
		t.Fatal("a 403 was not an error")
	}
	if strings.Contains(err.Error(), "mc-token") {
		t.Errorf("error disclosed the access token: %v", err)
	}
}

func TestJoinHonoursCancellation(t *testing.T) { ... }

func TestProfileReturnsTheAuthenticatedIdentity(t *testing.T) { ... }
```

Write the two sketched bodies in full.

- [ ] **Step 2: Run and verify failure**

- [ ] **Step 3: Implement**

`Join` posts `accessToken`, `selectedProfile` as the **undashed** UUID, and
`serverId` as `hash.String()`. A 204 is success; anything else is an error
naming the status and not the body.

M3's client check proved the server-side half of this against the real Mojang
session server and recorded why that mattered: every automated test stubs the
call, and a hash wrong in the same way on both sides of a loopback test still
passes. The same applies here, which is what Task 7's manual test is for.

- [ ] **Step 4: Run and verify it passes**

- [ ] **Step 5: Commit**

```bash
git add auth/microsoft/join.go auth/microsoft/join_test.go
git commit -m "feat(auth): join the session server with a Microsoft account"
```

### Task 6: The provider

**Files:**
- Create: `auth/microsoft/provider.go`, `auth/microsoft/provider_test.go`

**Interfaces:**
- Produces: `New(clientID string, opts ...Option) (auth.Provider, error)`, `WithTokenStore`, `WithHTTPClient`, `WithDeviceCodeCallback`, `WithEndpoints`.

- [ ] **Step 1: Write the failing test**

```go
func TestFirstRunAsksAPersonAndStoresTheResult(t *testing.T) {
	// No stored tokens: the callback must fire with a URL and code, and the
	// resulting tokens must be saved.
	...
}

func TestStoredValidTokensSkipTheDeviceCodeEntirely(t *testing.T) {
	// A second run with unexpired tokens must not call the device-code
	// endpoint and must not invoke the callback.
	...
}

func TestStoredExpiredTokensRefreshWithoutAPerson(t *testing.T) { ... }

func TestFailedRefreshFallsBackToTheDeviceCode(t *testing.T) { ... }

func TestNewRejectsAnEmptyClientID(t *testing.T) {
	if _, err := microsoft.New(""); err == nil {
		t.Fatal("New accepted an empty client ID")
	}
}

func TestNewRejectsANilCallbackWithNoStore(t *testing.T) {
	// Without a callback and without stored tokens there is no way to
	// complete a first login, and failing at construction beats failing
	// halfway through one.
	if _, err := microsoft.New("client-id"); err == nil {
		t.Fatal("New accepted a configuration that cannot complete a first login")
	}
}
```

Write the four sketched bodies in full. Each drives a fake endpoint set through
`WithEndpoints` and asserts on which endpoints were called.

- [ ] **Step 2: Run and verify failure**

- [ ] **Step 3: Implement**

`Authenticate` runs: load from the store; if present and fresh, skip to step 3
of the chain; if present and stale, refresh and on failure fall through; if
absent, request a device code, invoke the callback, and poll. Then Xbox Live,
XSTS, Minecraft login, profile. Save the Microsoft tokens at the point they are
obtained, not at the end, so a later failure does not discard a login a person
just completed.

`WithEndpoints` exists for tests and is documented as such. Default it to the
real endpoints as package constants.

- [ ] **Step 4: Run and verify it passes**

- [ ] **Step 5: Commit**

```bash
git add auth/microsoft/provider.go auth/microsoft/provider_test.go
git commit -m "feat(auth): add the Microsoft device-code provider"
```

### Task 7: The manual check, documentation, and the gate

**Files:**
- Create: `auth/microsoft/manual_test.go`
- Modify: `README.md`, `CHANGELOG.md`, `Taskfile.yml`, `../headless-minecraft/MASTER_PLAN.md`

- [ ] **Step 1: Add the opt-in real-account test**

```go
//go:build manual

package microsoft_test

// TestManualDeviceCodeLogin runs a real device-code login. It is behind a
// build tag and an environment variable so CI can never run it.
//
//	MC_MANUAL_CLIENT_ID=<azure app id> devbox run -- go test -tags manual ./auth/microsoft
//
// It stores nothing, prints the verification URL and user code through the
// callback, and asserts only that a profile came back. Do not add assertions
// on the account: whoever runs this uses their own.
func TestManualDeviceCodeLogin(t *testing.T) { ... }
```

Write the body in full. It must skip, not fail, when the environment variable
is unset.

- [ ] **Step 2: Document**

README gains an authentication section: offline versus Microsoft, the Azure
application registration the client ID comes from, that the library never
prints and the application supplies a callback, that no token is persisted
without a store, and the XSTS error codes with what each means for a person.

State plainly that the library uses the system trust store and never disables
certificate verification.

- [ ] **Step 3: Run the gate**

```bash
devbox run -- task verify
devbox run -- task secrets
```

`secrets` must be green. If `gitleaks` flags a test fixture, the fixture holds
something that looks like a credential — replace it with an obvious
placeholder rather than adding an allowlist entry.

- [ ] **Step 4: Confirm no test reaches the network**

```bash
devbox run -- go test -count=1 ./auth/... 2>&1 | tail -5
```

Run it with networking unavailable if the environment allows. Every test must
pass, because every endpoint is an `httptest` server.

- [ ] **Step 5: Update the milestone record**

Mark M6.4 complete in `MASTER_PLAN.md` and record whether the manual check was
run, against which account type, and what it found. An unrun manual check is
"client checks pending", not "complete".

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "docs: record Microsoft device-code authentication"
```

---

## Self-review notes

- **Several test bodies are sketched with `...` and marked "write in full".**
  They are the repetitive ones — four provider paths, three Xbox cases, two
  join cases — where the shape is set by a sibling test in the same file. Write
  them before the implementation, and do not commit a `...`.
- **The XSTS code table is the part most likely to be wrong and hardest to
  test.** Every entry is from published behavior, not from a run against a real
  account, and only Task 7's manual check exercises any of them. Treat a
  surprising code in the field as a table gap rather than an authentication
  failure.
- **`WithEndpoints` is a test seam in the public API.** That is a real cost.
  The alternative — an unexported field set through an internal test package —
  would keep the surface smaller, and is worth taking if the package ever grows
  a stable public contract. It is documented as a test seam so nobody builds on
  it.

# Persistent Chat and Account

`/account` and Ctrl+P select Account in the existing Bubble Tea program. Escape
returns to Chat and clears unfinished credential entry. Arrow keys select,
Enter activates, and confirmed device revocation or logout requires Enter/Y;
N cancels. Device details include activity, expiration and a current-device flag.
R refreshes devices. Both existing appearance modes retain their Chat formatting.
PgUp/PgDn scroll account details when long device names or narrow windows wrap.

Account requests have loading and result feedback. Repeated keys are ignored
while a request is in flight, including Escape/Ctrl+C: this bounded wait ensures
that a mutation's replacement token or recovery codes are handled before leaving.
Recovery codes use Up/Down to scroll and **A** to acknowledge saving all codes.
Escape, Ctrl+C, Ctrl+P and F2 cannot dismiss those codes. Update installation is
available in Chat; update checks and notifications continue on Account.

## Architecture and lifecycle

- `Model` owns navigation, composer, history, connection and background events.
  Page identifiers reserve space for Settings and administration; neither has a
  user-facing implementation in this change.
- `account.go` owns account screens, selection, confirmation and bounded layout.
- `credentials.go` owns dedicated credential buffers and input filtering.
- `account_service.go` adapts existing client account operations to asynchronous
  Bubble Tea commands and handles persistence and connection replacement.
- `Store.Snapshot/SetToken` synchronize tokens with background cursor writes.
  `Client.CloseAndWait` joins the old reader before updating credentials.

Ordinary navigation neither reconnects nor changes queued chat messages. Account
operations retain the existing short-lived authenticated WebSocket protocol.
After password replacement, the old chat client is stopped and joined, the new
token is saved with the latest cursor, and a new client is attached to the same
model. Late events are checked against their source channel. History is retained
and deduplicated; the composer and scroll remain intact. This reconnect cannot
guarantee delivery of messages already queued on the old transport (the existing
protocol has no client message acknowledgment/replay mechanism).

Revoking this device follows logout semantics. Successful logout stops the reader
before clearing the token/cursor and shows a signed-out screen. Network failures
leave the interface usable for retry. A credential-save failure stops chat and
requires login; the old credential file may remain on disk if it cannot be
replaced, but the process clears its in-memory token and does not reuse it.

## Credential security

The pinned Bubble Tea v2.0.10 runtime calls `MakeRaw` on terminal input on Windows
and Unix before accepting input, which disables OS echo and canonical input.
Windows uses the existing read/write CONIN$/CONOUT$ handles. Unix now fails closed
unless stdin and stdout are terminals. Raw-mode initialization errors abort the
program. No shell is used for credential input and no password text is rendered:
the view always shows the fixed `[hidden input]` placeholder, with no length hint.

Credentials bypass the normal textinput component entirely. Modifier shortcuts,
key releases and bracketed paste cannot reach the composer while Account is
active. Paste is intentionally disabled in credential forms. Buffers have a
128-byte cap, are wiped on cancel/mismatch, and move out of model state into the
command at submission. The command wipes them on return. New passwords require
12–128 bytes and matching confirmation, as in the standalone CLI.

Account failures use fixed messages rather than server error strings, preventing
an echoed credential in a server error from entering terminal output. Neither
passwords nor recovery codes are logged or automatically saved. Recovery codes
appear only in the alternate screen after successful generation and their slice
is cleared on acknowledgment. Cleanup clears sensitive model state on normal exit.

Go strings and JSON/network libraries necessarily make transient immutable copies;
this is not a claim of guaranteed memory zeroization. Terminal recording, a
compromised terminal, forced process termination or external signals can bypass
visual recovery-code acknowledgment; codes cannot be recovered after process loss.
Do not enable event/model dumps or terminal recording during credential use.

## Verification and remaining manual checks

Automated tests cover navigation, drafts/scroll, incoming messages, update offers,
resizing, input isolation, buffer wiping, password validation, session selection,
confirmation, duplicate requests, logout/current revocation, save/network failures,
stale events and recovery acknowledgment. A loopback WebSocket peer verifies that
navigation keeps one chat login and token replacement resumes from the saved cursor.
It never connects to Neon or reads production account credentials.

Before release, exercise Windows Terminal/PowerShell and Linux terminal emulators
with disposable accounts: verify no echo or password scrollback, Unicode editing,
paste rejection, resize during entry/codes, Ctrl+C, terminal disconnect, failed
disk writes, server heartbeat revocation, and update notifications in Account.
Test both appearance modes and narrow terminals (minimum supported layout 20×8).
Run the PostgreSQL integration suite only against a verified disposable database.

Verified on the Windows development host for this change:

- `gofmt` and `git diff --check`: passed.
- `go test ./... -skip TestPostgreSQLIntegration -count=1`: passed, including
  existing server/transport integration tests and the new account WebSocket test.
- `go vet ./...`: passed.
- Windows client build (`dist/sockt-account.exe`): passed.
- Linux amd64 `go build ./...` and TUI test-binary compilation: passed. These are
  cross-compilation checks, not Linux execution or interactive terminal tests.
- Race check attempted: unavailable because CGO is disabled and no C compiler was
  found. Run it on a host with a supported C toolchain before release.
- PostgreSQL integration deliberately excluded: no verified disposable database
  is running; Docker's daemon is unavailable. No production service was contacted.

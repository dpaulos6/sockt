# Sockt v0.9 — slash command completion

Slash commands run locally in the Bubble Tea client and are never sent to the
chat server. This UI-only addition can be merged into the existing v0.9 branch
**before** its first signed client release; it requires no database migration
or additional server changes beyond the existing v0.9 account upgrade.

## Using commands

- Type `/` at the start of the message box to see the available commands.
- Type a prefix such as `/ac` to filter the menu to `/account`; matching ignores
  case. Use Up/Down to select a command.
- Tab fills the selected command but doesn't run it. Press Enter to execute.
- Enter on an incomplete command fills the selected suggestion first; another
  Enter runs it. Enter on an exact command runs it immediately.
- Escape dismisses suggestions, leaving your message untouched. Editing the
  input reopens the menu when the new text starts with `/`.
- Unknown commands (for example `/test`) show "No matching commands". Pressing
  Enter shows a local error, preserving what you typed. No unknown slash-prefixed
  text can accidentally be sent to general chat.

| Command   | Action |
|-----------|--------|
| `/account` | Opens the existing v0.9 account-management menu |
| `/help`    | Displays keyboard shortcuts |
| `/update`  | Offers to install an available signed update, otherwise checks for updates |
| `/exit`    | Closes the chat |

The existing F1, F2, Ctrl+P, PgUp/PgDn and Ctrl+C shortcuts still work.
The suggestion panel automatically shrinks on short terminals so the composer
stays visible. The system currently accepts no slash command arguments.

## Dev testing (Windows PowerShell, repository root)

Apply the patch on `feat/accounts-v09` with a clean working tree. Run:

```powershell
go fmt ./...
go vet ./...
go test ./...
go build ./...
```

Confirm the UI works in a **local or disposable test server**: `/`, `/ac`,
`/test` and Ctrl+P. Don't publish a release or update your live Neon database
just to test this UI addition.

When v0.9 passes its database, recovery and session tests, build and sign the
v0.9.0 client using the **original v0.8 signing key**, then upload release files
and update `stable.json` last. Only already-installed v0.8 clients can update
in-app; v0.7 clients still need a one-time manual updater installation.

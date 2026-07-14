# internal/auth

## Role

Implements `AUTH` credential checking for the RESP server: either a single shared
password (`New`, Redis's classic `requirepass` behavior, present since v0.1.0) or real
per-workspace credentials (`NewMultiWorkspace`, shipped v0.7.0 via
`--workspace-credentials`), where the password a client supplies to `AUTH` also selects
which workspace that connection is allowed to touch. There is still no ACL/user model —
just these two modes.

This package only answers "is this password valid, and if so for which workspace." It
does not itself enforce anything against a live connection — enforcement is
`ClientState.authorizedForWorkspace` in `internal/server/resp/client.go`, a different
package (don't rewrite its docs here, just know it exists: it calls
`Auth.MultiWorkspace()` and, in that mode, compares the command's target workspace
against the connection's already-authenticated `cs.Workspace`).

## Contract

- `Authenticator` operates in exactly one of two mutually exclusive modes, fixed at
  construction: single-password (`New(password)`) or multi-workspace
  (`NewMultiWorkspace(creds ...Credential)`). There's no way to switch modes on an
  existing `*Authenticator`, and `internal/server/server.go` picks one at startup based
  on whether `cfg.WorkspaceCredentials` is non-empty — the two flags are mutually
  exclusive at the config level too.
- `Credential{Workspace, Password}` is the input shape for multi-workspace mode.
- `WorkspaceForPassword(password) (string, bool)` is multi-workspace-only — always
  returns `("", false)` in single-password mode. This is what the RESP layer's `AUTH`
  handler calls to learn *which* workspace a password grants, not just whether it's
  valid.
- `Check(password) bool` answers "is this password valid at all" in either mode — in
  multi-workspace mode it matches *any* configured credential, without telling you
  which workspace. Callers that need the workspace must use `WorkspaceForPassword`
  instead, not infer it from `Check`.
- `Required() bool`: single-password mode requires AUTH only if a password is
  configured (empty password = auth disabled, matching Redis's no-requirepass default).
  Multi-workspace mode *always* requires AUTH, even with zero configured credentials —
  there is no unauthenticated default workspace once multi-workspace mode is selected.
- **Non-obvious invariant**: `NewMultiWorkspace` does not error or warn on two
  credentials sharing the same password (same or different workspace) — that's a real
  misconfiguration (ambiguous which workspace `AUTH` with that password selects), but
  this constructor resolves it silently by last-one-wins per `creds`' input order (a
  plain left-to-right map insertion in `byPassword`). If you add config-level validation
  that should reject duplicate passwords, do it at the call site in
  `internal/server/server.go`, not inside this constructor — its contract deliberately
  tolerates the ambiguity.

## Testing

```
go test ./internal/auth/... -race
```

No test doubles — `auth_test.go` exercises `Authenticator` directly. `-race` is
unlikely to catch anything (no concurrent mutation inside `Authenticator` after
construction — `byPassword`/`password` are read-only post-`New`/`NewMultiWorkspace`),
but it's free and consistent with the rest of the suite; no reason to skip it.

## Known gaps

- `NewMultiWorkspace` intentionally does not validate its input for duplicate passwords
  at construction time (see invariant above) — this is a documented tolerance, not an
  oversight, but it means a duplicate-password misconfiguration fails silently rather
  than at startup.
- The native MCP server has no auth/workspace enforcement at all, even when
  `--workspace-credentials` is configured — this package's checks are RESP-only. See
  root AGENTS.md's "Known, honest gaps."

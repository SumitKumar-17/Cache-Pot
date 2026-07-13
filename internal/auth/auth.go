// Package auth implements Cache-Pot's AUTH-checking. Phase 1 shipped a
// single global shared password (Redis's classic requirepass behavior);
// Phase 7 adds an alternative multi-workspace mode, where each workspace has
// its own password and AUTH selects which workspace a connection operates
// against. There is still no ACL/user model -- that remains a later
// concern.
package auth

// Credential is one workspace's own AUTH password, for multi-workspace mode
// (see NewMultiWorkspace).
type Credential struct {
	Workspace string
	Password  string
}

// Authenticator checks a client-supplied password against the server's
// configured credentials. It operates in exactly one of two modes,
// determined by how it was constructed:
//
//   - single-password mode (New): one global password (or none), matching
//     Phase 1's original requirepass-style behavior. This is the default for
//     every existing deployment and is unchanged by Phase 7.
//   - multi-workspace mode (NewMultiWorkspace): a set of per-workspace
//     passwords, where AUTH must match one of them and thereby selects that
//     connection's workspace.
type Authenticator struct {
	// password is the single global password in single-password mode.
	// Unused (always "") in multi-workspace mode.
	password string

	// multiWorkspace is true when this Authenticator was built via
	// NewMultiWorkspace, regardless of whether byPassword ended up empty
	// (an empty credential list still means "multi-workspace mode, no
	// password grants access" rather than falling back to single-password
	// semantics).
	multiWorkspace bool

	// byPassword maps a workspace's password to that workspace, used only
	// in multi-workspace mode. If two configured credentials share the same
	// password (whether for the same or different workspaces), the last one
	// passed to NewMultiWorkspace wins -- this map is built by a simple
	// left-to-right insertion, so later entries overwrite earlier ones.
	byPassword map[string]string
}

// New builds an Authenticator for the given configured password. An empty
// password means auth is disabled (matches Redis's default: no requirepass
// configured).
func New(password string) *Authenticator {
	return &Authenticator{password: password}
}

// NewMultiWorkspace builds an Authenticator in per-workspace mode: AUTH must
// supply a password matching one configured credential, which selects that
// connection's workspace. Required() always reports true in this mode --
// there is no unauthenticated "default" workspace the way there is in
// single-password mode, since every connection must authenticate to select
// which workspace it's even allowed to touch.
//
// Two credentials sharing the same password (for the same or different
// workspaces) is a real misconfiguration -- it's ambiguous which workspace
// AUTH with that password would select. This constructor doesn't error on
// it (callers wanting a startup error should validate their own input
// before calling this); it resolves deterministically by last-one-wins, per
// creds' order.
func NewMultiWorkspace(creds ...Credential) *Authenticator {
	byPassword := make(map[string]string, len(creds))
	for _, c := range creds {
		byPassword[c.Password] = c.Workspace
	}
	return &Authenticator{multiWorkspace: true, byPassword: byPassword}
}

// MultiWorkspace reports whether this Authenticator was built via
// NewMultiWorkspace.
func (a *Authenticator) MultiWorkspace() bool {
	return a.multiWorkspace
}

// WorkspaceForPassword looks up which workspace the given password grants
// access to. Only meaningful in multi-workspace mode -- it always returns
// ("", false) in single-password mode.
func (a *Authenticator) WorkspaceForPassword(password string) (string, bool) {
	if !a.multiWorkspace {
		return "", false
	}
	ws, ok := a.byPassword[password]
	return ws, ok
}

// Required reports whether a client must AUTH before running other
// commands. Always true in multi-workspace mode (see NewMultiWorkspace);
// in single-password mode, true only when a password is configured.
func (a *Authenticator) Required() bool {
	if a.multiWorkspace {
		return true
	}
	return a.password != ""
}

// Check reports whether the supplied password matches the configured one.
// This is single-password mode's check: if no password is configured, Check
// always returns true (matching Redis: AUTH against a server with no
// requirepass errors at the caller level, but any connection is already
// usable without AUTH). In multi-workspace mode, Check reports whether
// password matches ANY configured credential -- callers that need to know
// *which* workspace it selects should use WorkspaceForPassword instead.
func (a *Authenticator) Check(password string) bool {
	if a.multiWorkspace {
		_, ok := a.byPassword[password]
		return ok
	}
	if a.password == "" {
		return true
	}
	return password == a.password
}

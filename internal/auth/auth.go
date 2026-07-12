// Package auth implements Phase 1 authentication: a single configured
// password checked by the AUTH command, matching Redis's classic
// requirepass behavior. There is no ACL/user model yet — that is a later
// phase's concern.
package auth

// Authenticator checks a client-supplied password against the server's
// configured password.
type Authenticator struct {
	password string
}

// New builds an Authenticator for the given configured password. An empty
// password means auth is disabled (matches Redis's default: no requirepass
// configured).
func New(password string) *Authenticator {
	return &Authenticator{password: password}
}

// Required reports whether a client must AUTH before running other commands.
func (a *Authenticator) Required() bool {
	return a.password != ""
}

// Check reports whether the supplied password matches the configured one.
// If no password is configured, Check always returns true (matching Redis:
// AUTH against a server with no requirepass errors at the caller level, but
// any connection is already usable without AUTH).
func (a *Authenticator) Check(password string) bool {
	if a.password == "" {
		return true
	}
	return password == a.password
}

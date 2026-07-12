package server

// Config holds the Phase 1 server configuration. cmd/cachepotd builds one of
// these from CLI flags with environment-variable fallback and passes it to
// Run.
type Config struct {
	// Port is the TCP port the RESP listener binds to.
	Port int
	// Password, if non-empty, must be supplied via AUTH before other
	// commands are allowed (matches Redis's classic requirepass). Empty
	// means no authentication is required, matching Redis's default.
	Password string
	// MaxConnections bounds the number of concurrent client connections;
	// connections beyond this are rejected with a clean error reply and
	// the socket is closed, rather than being allowed to degrade service.
	MaxConnections int
}

const (
	// DefaultPort is deliberately not 6379, Redis's default, so cachepotd
	// doesn't collide with a real local Redis during development/testing.
	DefaultPort           = 6380
	DefaultMaxConnections = 10000
)

// DefaultConfig returns the Phase 1 default configuration (no auth
// required).
func DefaultConfig() Config {
	return Config{
		Port:           DefaultPort,
		MaxConnections: DefaultMaxConnections,
	}
}

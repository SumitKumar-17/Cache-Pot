package resp

import "strings"

// HandlerFunc implements one RESP command. args[0] is the command name as
// sent by the client (original casing); args[1:] are its arguments.
type HandlerFunc func(cs *ClientState, args []string) Reply

// Command describes one registered command: its handler plus enough
// metadata for dispatch to do arity checking, auth gating, and MULTI
// queueing decisions without each handler re-implementing that logic.
type Command struct {
	Name string
	// MinArgs/MaxArgs count the command name itself as args[0]. MaxArgs
	// -1 means unbounded.
	MinArgs int
	MaxArgs int
	// AllowedNoAuth marks commands usable before AUTH succeeds (AUTH,
	// HELLO, QUIT, RESET).
	AllowedNoAuth bool
	// NoQueue marks commands that always execute immediately even inside a
	// MULTI block, instead of being queued (MULTI, EXEC, DISCARD, WATCH,
	// UNWATCH, QUIT, RESET, SUBSCRIBE-family).
	NoQueue bool
	Handler HandlerFunc
}

func (c *Command) arityOK(n int) bool {
	if n < c.MinArgs {
		return false
	}
	if c.MaxArgs >= 0 && n > c.MaxArgs {
		return false
	}
	return true
}

// Registry is a case-insensitive command table: the map[string]HandlerFunc
// (plus metadata) that command dispatch is built around.
type Registry struct {
	commands map[string]*Command
}

// NewRegistry builds an empty Registry.
func NewRegistry() *Registry {
	return &Registry{commands: make(map[string]*Command)}
}

// Register adds cmd to the registry, keyed case-insensitively by cmd.Name.
func (r *Registry) Register(cmd *Command) {
	r.commands[strings.ToUpper(cmd.Name)] = cmd
}

// Lookup finds a command by name (case-insensitive).
func (r *Registry) Lookup(name string) (*Command, bool) {
	c, ok := r.commands[strings.ToUpper(name)]
	return c, ok
}

// Handle is the single entry point connection handling calls per command:
// it looks the command up, checks auth, decides whether to queue it inside
// a MULTI block, checks arity, and finally invokes the handler. Centralizing
// this here (rather than duplicating it in conn.go) keeps MULTI queueing
// and auth gating consistent regardless of call site.
func (r *Registry) Handle(cs *ClientState, args []string) Reply {
	if len(args) == 0 {
		return nil
	}
	name := strings.ToUpper(args[0])
	cmd, ok := r.commands[name]
	if !ok {
		if cs.InMulti {
			cs.MultiError = true
		}
		return Err(ErrUnknownCommand(args[0], args[1:]))
	}

	if cs.Deps.Auth.Required() && !cs.Authenticated && !cmd.AllowedNoAuth {
		return Err(ErrNoAuthMsg)
	}

	if cs.InMulti && !cmd.NoQueue {
		if !cmd.arityOK(len(args)) {
			cs.MultiError = true
			return Err(ErrWrongNumberOfArgs(cmd.Name))
		}
		cs.Queued = append(cs.Queued, append([]string(nil), args...))
		return Simple("QUEUED")
	}

	if !cmd.arityOK(len(args)) {
		return Err(ErrWrongNumberOfArgs(cmd.Name))
	}
	return cmd.Handler(cs, args)
}

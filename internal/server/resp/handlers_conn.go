package resp

import (
	"fmt"
	"strconv"
	"strings"
)

// RegisterConn adds the connection/protocol-management commands: PING,
// ECHO, SELECT, HELLO, AUTH, CLIENT, COMMAND, INFO, QUIT.
func RegisterConn(r *Registry) {
	r.Register(&Command{Name: "PING", MinArgs: 1, MaxArgs: 2, Handler: handlePing})
	r.Register(&Command{Name: "ECHO", MinArgs: 2, MaxArgs: 2, Handler: handleEcho})
	r.Register(&Command{Name: "SELECT", MinArgs: 2, MaxArgs: 2, Handler: handleSelect})
	r.Register(&Command{Name: "HELLO", MinArgs: 1, MaxArgs: -1, AllowedNoAuth: true, Handler: handleHello})
	r.Register(&Command{Name: "AUTH", MinArgs: 2, MaxArgs: 3, AllowedNoAuth: true, Handler: handleAuth})
	r.Register(&Command{Name: "CLIENT", MinArgs: 2, MaxArgs: -1, Handler: handleClient})
	r.Register(&Command{Name: "COMMAND", MinArgs: 1, MaxArgs: -1, AllowedNoAuth: true, Handler: handleCommand})
	r.Register(&Command{Name: "INFO", MinArgs: 1, MaxArgs: 2, AllowedNoAuth: true, Handler: handleInfo})
	r.Register(&Command{Name: "QUIT", MinArgs: 1, MaxArgs: 1, AllowedNoAuth: true, NoQueue: true, Handler: handleQuit})
}

func handlePing(cs *ClientState, args []string) Reply {
	if len(args) == 2 {
		return BulkString(args[1])
	}
	return Simple("PONG")
}

func handleEcho(cs *ClientState, args []string) Reply {
	return BulkString(args[1])
}

func handleSelect(cs *ClientState, args []string) Reply {
	n, err := strconv.Atoi(args[1])
	if err != nil {
		return Err(ErrNotIntegerMsg)
	}
	if n != 0 {
		return Err(ErrWrongDBMsg)
	}
	return OK
}

// handleHello implements HELLO for Phase 1: no-arg or "HELLO 2" (optionally
// followed by AUTH/SETNAME options) succeeds with a minimal server-info
// reply. Any other protocol version — most importantly "HELLO 3" — returns
// a clean RESP2 NOPROTO error rather than hanging or misbehaving, since
// real client libraries probe this on connect.
func handleHello(cs *ClientState, args []string) Reply {
	i := 1
	proto := "2"
	if i < len(args) {
		if _, err := strconv.Atoi(args[i]); err == nil {
			proto = args[i]
			i++
		}
	}

	for i < len(args) {
		opt := strings.ToUpper(args[i])
		switch opt {
		case "AUTH":
			if i+2 >= len(args) {
				return Err(ErrSyntaxMsg)
			}
			password := args[i+2] // args[i+1] is username; no ACL in Phase 1
			if cs.Deps.Auth.MultiWorkspace() {
				workspace, ok := cs.Deps.Auth.WorkspaceForPassword(password)
				if !ok {
					return Err(ErrInvalidPasswordMsg)
				}
				cs.Authenticated = true
				cs.Workspace = workspace
			} else {
				if !cs.Deps.Auth.Check(password) {
					return Err(ErrInvalidPasswordMsg)
				}
				cs.Authenticated = true
			}
			i += 3
		case "SETNAME":
			if i+1 >= len(args) {
				return Err(ErrSyntaxMsg)
			}
			cs.Name = args[i+1]
			i += 2
		default:
			return Err(ErrSyntaxMsg)
		}
	}

	if proto != "2" {
		return Err(ErrNoProtoMsg)
	}
	if cs.Deps.Auth.Required() && !cs.Authenticated {
		return Err(ErrNoAuthMsg)
	}

	return ArraySlice([]Reply{
		BulkString("server"), BulkString("cachepot"),
		BulkString("version"), BulkString("0.1.0"),
		BulkString("proto"), Int(2),
		BulkString("id"), Int(1),
		BulkString("mode"), BulkString("standalone"),
		BulkString("role"), BulkString("master"),
		BulkString("modules"), ArraySlice(nil),
	})
}

func handleAuth(cs *ClientState, args []string) Reply {
	var password string
	switch len(args) {
	case 2:
		password = args[1]
	case 3:
		password = args[2] // args[1] is username; no ACL in Phase 1
	}
	if !cs.Deps.Auth.Required() {
		return Err("ERR Client sent AUTH, but no password is set. Did you mean AUTH <username> <password>?")
	}
	if cs.Deps.Auth.MultiWorkspace() {
		workspace, ok := cs.Deps.Auth.WorkspaceForPassword(password)
		if !ok {
			return Err(ErrInvalidPasswordMsg)
		}
		cs.Authenticated = true
		cs.Workspace = workspace
		return OK
	}
	if !cs.Deps.Auth.Check(password) {
		return Err(ErrInvalidPasswordMsg)
	}
	cs.Authenticated = true
	return OK
}

func handleClient(cs *ClientState, args []string) Reply {
	sub := strings.ToUpper(args[1])
	switch sub {
	case "GETNAME":
		return BulkString(cs.Name)
	case "SETNAME":
		if len(args) != 3 {
			return Err(ErrWrongNumberOfArgs("client|setname"))
		}
		cs.Name = args[2]
		return OK
	default:
		return Err(fmt.Sprintf("ERR Unknown subcommand or wrong number of arguments for '%s'", args[1]))
	}
}

// handleCommand is a minimal COMMAND implementation: enough for clients
// that probe it on connect (e.g. to check for command existence) not to
// break, without a full command-metadata table.
func handleCommand(cs *ClientState, args []string) Reply {
	if len(args) >= 2 && strings.EqualFold(args[1], "COUNT") {
		return Int(int64(len(cs.Deps.Registry.commands)))
	}
	return ArraySlice(nil)
}

func handleInfo(cs *ClientState, args []string) Reply {
	snap := cs.Deps.Metrics.Snapshot()
	info := "# Server\r\n" +
		"redis_version:7.0.0\r\n" +
		"cachepot_version:0.1.0\r\n" +
		"\r\n# Clients\r\n" +
		fmt.Sprintf("connected_clients:%d\r\n", snap.ConnectionsActive) +
		"\r\n# Stats\r\n" +
		fmt.Sprintf("total_connections_received:%d\r\n", snap.ConnectionsTotal) +
		fmt.Sprintf("total_commands_processed:%d\r\n", snap.CommandsTotal) +
		fmt.Sprintf("rejected_connections:%d\r\n", snap.ConnectionsRejected)
	return BulkString(info)
}

func handleQuit(cs *ClientState, args []string) Reply {
	cs.Quit = true
	return OK
}

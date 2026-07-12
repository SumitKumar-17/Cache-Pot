package resp

import (
	"errors"
	"fmt"
	"strings"

	"github.com/SumitKumar-17/cache-pot/internal/storage"
)

// This file centralizes the exact Redis-shaped error strings Phase 1
// commands can produce, so wording stays consistent across handlers and
// matches what real Redis clients expect to see/parse.

// ErrWrongTypeMsg is the standard Redis WRONGTYPE error text.
const ErrWrongTypeMsg = "WRONGTYPE Operation against a key holding the wrong kind of value"

// ErrWrongNumberOfArgs formats Redis's arity error for cmd.
func ErrWrongNumberOfArgs(cmd string) string {
	return fmt.Sprintf("ERR wrong number of arguments for '%s' command", strings.ToLower(cmd))
}

// ErrNotIntegerMsg matches Redis's integer-parse error text.
const ErrNotIntegerMsg = "ERR value is not an integer or out of range"

// ErrNotFloatMsg matches Redis's float-parse error text.
const ErrNotFloatMsg = "ERR value is not a valid float"

// ErrSyntaxMsg matches Redis's generic syntax error text.
const ErrSyntaxMsg = "ERR syntax error"

// ErrNoAuthMsg matches Redis's "must AUTH first" error text.
const ErrNoAuthMsg = "NOAUTH Authentication required."

// ErrInvalidPasswordMsg matches Redis's AUTH failure text.
const ErrInvalidPasswordMsg = "WRONGPASS invalid username-password pair or user is disabled."

// ErrNoProtoMsg is returned for HELLO requests asking for an unsupported
// protocol version (Phase 1 only supports RESP2).
const ErrNoProtoMsg = "NOPROTO unsupported protocol version"

// ErrWrongDBMsg is returned by SELECT for any database index other than 0
// (Phase 1 supports only a single logical database).
const ErrWrongDBMsg = "ERR DB index is out of range"

// ErrNoSuchKeyMsg matches Redis's "no such key" text (e.g. RENAME on a
// missing source key).
const ErrNoSuchKeyMsg = "ERR no such key"

// ErrUnknownCommand formats Redis's unknown-command error, including a
// preview of the offending arguments.
func ErrUnknownCommand(name string, args []string) string {
	parts := make([]string, len(args))
	for i, a := range args {
		parts[i] = "'" + a + "'"
	}
	return fmt.Sprintf("ERR unknown command '%s', with args beginning with: %s", name, strings.Join(parts, ", "))
}

// ErrFromStorage translates an error returned by storage.Engine into a
// Redis-shaped Reply. It is the single place RESP handlers funnel storage
// errors through, so every handler produces identically worded errors for
// the same underlying condition.
func ErrFromStorage(err error) Reply {
	switch {
	case errors.Is(err, storage.ErrWrongType):
		return Err(ErrWrongTypeMsg)
	case errors.Is(err, storage.ErrNotInteger):
		return Err(ErrNotIntegerMsg)
	case errors.Is(err, storage.ErrNotFloat):
		return Err(ErrNotFloatMsg)
	case errors.Is(err, storage.ErrIndexOutOfRange):
		return Err("ERR index out of range")
	case errors.Is(err, storage.ErrNoSuchKey):
		return Err(ErrNoSuchKeyMsg)
	default:
		return Err("ERR " + err.Error())
	}
}

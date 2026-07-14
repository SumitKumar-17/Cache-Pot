// Package resp implements the RESP2 wire protocol (encoding + decoding),
// command dispatch, and every command handler. RESP3 is explicitly out of
// scope (see handlers_conn.go's HELLO handling).
package resp

import (
	"bufio"
	"errors"
	"io"
	"strconv"
	"strings"
)

// ErrProtocol indicates malformed RESP framing on the wire.
var ErrProtocol = errors.New("resp: protocol error")

// ReadCommand reads one client command from r. Most real clients send
// commands as a RESP array of bulk strings (e.g. `*2\r\n$3\r\nGET\r\n$1\r\nx\r\n`);
// this package also accepts simple inline commands (a bare line of
// whitespace-separated words, no RESP framing) since some tools/tests talk
// to a RESP server that way. An empty returned slice with a nil error means
// "blank line, no command" and the caller should just read again.
func ReadCommand(r *bufio.Reader) ([]string, error) {
	b, err := r.Peek(1)
	if err != nil {
		return nil, err
	}
	if b[0] == '*' {
		return readArrayCommand(r)
	}
	return readInlineCommand(r)
}

func readLine(r *bufio.Reader) (string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func readArrayCommand(r *bufio.Reader) ([]string, error) {
	line, err := readLine(r)
	if err != nil {
		return nil, err
	}
	if len(line) < 1 || line[0] != '*' {
		return nil, ErrProtocol
	}
	n, err := strconv.Atoi(line[1:])
	if err != nil {
		return nil, ErrProtocol
	}
	if n <= 0 {
		return []string{}, nil
	}
	args := make([]string, 0, n)
	for i := 0; i < n; i++ {
		hdr, err := readLine(r)
		if err != nil {
			return nil, err
		}
		if len(hdr) < 1 || hdr[0] != '$' {
			return nil, ErrProtocol
		}
		blen, err := strconv.Atoi(hdr[1:])
		if err != nil {
			return nil, ErrProtocol
		}
		if blen < 0 {
			args = append(args, "")
			continue
		}
		buf := make([]byte, blen+2) // payload + trailing \r\n
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		args = append(args, string(buf[:blen]))
	}
	return args, nil
}

func readInlineCommand(r *bufio.Reader) ([]string, error) {
	line, err := readLine(r)
	if err != nil {
		return nil, err
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return []string{}, nil
	}
	return strings.Fields(line), nil
}

// Writer encodes RESP2 values onto an underlying io.Writer, buffering
// writes so a connection handler can batch several replies (e.g. for
// pipelined commands) before a single Flush.
type Writer struct {
	bw *bufio.Writer
}

// NewWriter wraps w in a buffered RESP2 Writer.
func NewWriter(w io.Writer) *Writer {
	return &Writer{bw: bufio.NewWriter(w)}
}

// Flush pushes any buffered bytes to the underlying writer.
func (w *Writer) Flush() error { return w.bw.Flush() }

// Buffered returns the number of bytes not yet flushed.
func (w *Writer) Buffered() int { return w.bw.Buffered() }

// WriteSimpleString writes a RESP2 simple string: "+<s>\r\n".
func (w *Writer) WriteSimpleString(s string) error {
	_, err := w.bw.WriteString("+" + s + "\r\n")
	return err
}

// WriteError writes a RESP2 error: "-<s>\r\n". s should already start with
// an error-kind word (ERR, WRONGTYPE, NOAUTH, ...).
func (w *Writer) WriteError(s string) error {
	_, err := w.bw.WriteString("-" + s + "\r\n")
	return err
}

// WriteInteger writes a RESP2 integer: ":<n>\r\n".
func (w *Writer) WriteInteger(n int64) error {
	_, err := w.bw.WriteString(":" + strconv.FormatInt(n, 10) + "\r\n")
	return err
}

// WriteBulk writes a RESP2 bulk string. A nil slice writes the null bulk
// string "$-1\r\n" (RESP2's representation of "no value").
func (w *Writer) WriteBulk(b []byte) error {
	if b == nil {
		_, err := w.bw.WriteString("$-1\r\n")
		return err
	}
	if _, err := w.bw.WriteString("$" + strconv.Itoa(len(b)) + "\r\n"); err != nil {
		return err
	}
	if _, err := w.bw.Write(b); err != nil {
		return err
	}
	_, err := w.bw.WriteString("\r\n")
	return err
}

// WriteArrayHeader writes a RESP2 array header: "*<n>\r\n". n == -1 writes
// the null array header ("*-1\r\n"); callers must not write any elements
// after a null array header.
func (w *Writer) WriteArrayHeader(n int) error {
	_, err := w.bw.WriteString("*" + strconv.Itoa(n) + "\r\n")
	return err
}

// Reply is a self-encoding RESP2 value: calling it writes the value to w.
// Handlers build replies compositionally, e.g. Array(Bulk(a), Bulk(b)).
type Reply func(w *Writer) error

// Simple builds a RESP2 simple-string reply.
func Simple(s string) Reply {
	return func(w *Writer) error { return w.WriteSimpleString(s) }
}

// Err builds a RESP2 error reply from an already Redis-shaped error string
// (see errors.go for the standard message builders).
func Err(msg string) Reply {
	return func(w *Writer) error { return w.WriteError(msg) }
}

// Int builds a RESP2 integer reply.
func Int(n int64) Reply {
	return func(w *Writer) error { return w.WriteInteger(n) }
}

// Bulk builds a RESP2 bulk-string reply. A nil slice becomes a null bulk
// string ($-1), matching a Redis "key not found" style reply.
func Bulk(b []byte) Reply {
	return func(w *Writer) error { return w.WriteBulk(b) }
}

// BulkString is a convenience wrapper for Bulk([]byte(s)).
func BulkString(s string) Reply {
	return Bulk([]byte(s))
}

// NullBulk is the RESP2 null bulk string reply ($-1).
func NullBulk() Reply { return Bulk(nil) }

// NullArray is the RESP2 null array reply (*-1), used e.g. by EXEC when a
// watched transaction aborts.
func NullArray() Reply {
	return func(w *Writer) error { return w.WriteArrayHeader(-1) }
}

// Array builds a RESP2 array reply from the given sub-replies.
func Array(items ...Reply) Reply {
	return ArraySlice(items)
}

// ArraySlice is Array taking a pre-built slice, useful when the reply count
// is computed dynamically.
func ArraySlice(items []Reply) Reply {
	return func(w *Writer) error {
		if err := w.WriteArrayHeader(len(items)); err != nil {
			return err
		}
		for _, it := range items {
			if err := it(w); err != nil {
				return err
			}
		}
		return nil
	}
}

// OK is the common "+OK" reply.
var OK = Simple("OK")

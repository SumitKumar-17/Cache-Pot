package resp

import (
	"bufio"
	"errors"
	"io"
	"net"
)

// HandleConn drives one client connection end to end: read a command,
// dispatch it, write the reply, repeat, until the client disconnects or
// sends QUIT. Pipelining is supported by only flushing the write buffer
// once the read buffer has no more immediately-available bytes (or the
// connection is closing) rather than after every single reply.
func HandleConn(conn net.Conn, deps *Deps) {
	defer conn.Close()

	deps.Metrics.ConnectionOpened()
	defer deps.Metrics.ConnectionClosed()

	reader := bufio.NewReader(conn)
	writer := NewWriter(conn)
	cs := NewClientState(deps, conn, writer)
	defer cleanupSubscriptions(cs)

	for {
		args, err := ReadCommand(reader)
		if err != nil {
			if !errors.Is(err, io.EOF) {
				deps.Logger.Debug("connection read error", "err", err)
			}
			return
		}
		if len(args) == 0 {
			continue
		}

		reply := deps.Registry.Handle(cs, args)
		deps.Metrics.CommandExecuted()
		if err := cs.writeReply(reply); err != nil {
			return
		}

		if cs.Quit {
			_ = cs.flush()
			return
		}

		if reader.Buffered() == 0 {
			if err := cs.flush(); err != nil {
				return
			}
		}
	}
}

package transport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/ailiheizi/restoreweave/client/command"
)

// ErrServerClosed is returned when the daemon closes the connection before a
// result frame arrives.
var ErrServerClosed = errors.New("connection closed by server before a result was returned")

// Conn is one Unix-socket connection to restoreweaved. It is single-use per
// round trip: Do writes one envelope and reads exactly one result frame.
type Conn struct {
	conn net.Conn
}

// Dial opens a connection to the daemon at socketPath.
func Dial(socketPath string) (*Conn, error) {
	return DialContext(context.Background(), socketPath)
}

// DialContext opens a connection to the daemon at socketPath, honoring the
// context deadline.
func DialContext(ctx context.Context, socketPath string) (*Conn, error) {
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", socketPath, err)
	}
	return &Conn{conn: conn}, nil
}

// Do sends one envelope and returns the matching result. The context deadline
// bounds the whole round trip; a nil-deadline context may block until the
// peer responds or closes the connection.
func (c *Conn) Do(ctx context.Context, env command.Envelope) (command.Result, error) {
	normalized, err := command.NormalizeEnvelope(env)
	if err != nil {
		return command.Result{}, err
	}
	if err := c.applyDeadline(ctx); err != nil {
		return command.Result{}, err
	}
	payload, err := json.Marshal(normalized)
	if err != nil {
		return command.Result{}, fmt.Errorf("encode envelope: %w", err)
	}
	if err := WriteFrame(c.conn, payload); err != nil {
		return command.Result{}, fmt.Errorf("write envelope: %w", err)
	}
	raw, err := ReadFrame(c.conn, MaxFrameBytes)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return command.Result{}, ErrServerClosed
		}
		return command.Result{}, fmt.Errorf("read result: %w", err)
	}
	var result command.Result
	if err := json.Unmarshal(raw, &result); err != nil {
		return command.Result{}, fmt.Errorf("decode result: %w", err)
	}
	return result, nil
}

// Close closes the underlying connection.
func (c *Conn) Close() error { return c.conn.Close() }

func (c *Conn) applyDeadline(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if deadline, ok := ctx.Deadline(); ok {
		return c.conn.SetDeadline(deadline)
	}
	return c.conn.SetDeadline(time.Time{})
}

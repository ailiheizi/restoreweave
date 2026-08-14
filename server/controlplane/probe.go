package controlplane

import (
	"context"
	"net"
	"time"
)

// socketProbe attempts a short-lived dial to decide whether an existing
// socket file still has a live listener.
func socketProbe(path string) (net.Conn, error) {
	dialer := net.Dialer{Timeout: 500 * time.Millisecond}
	return dialer.DialContext(context.Background(), "unix", path)
}

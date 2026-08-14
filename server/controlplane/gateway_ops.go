package controlplane

import (
	"context"
	"time"

	"github.com/ailiheizi/restoreweave/client/command"
)

const mountNotOurJob = "RestoreWeave does not mount snapshots. Restore with plan.restore, then attach the directory with rclone, sshfs, or the NAS share."

func (d *Dispatcher) handleGatewayMount(_ context.Context, env command.Envelope, started time.Time) command.Result {
	return failed(env, started, newReason(ReasonCodeUnimplemented, mountNotOurJob))
}

func (d *Dispatcher) handleGatewayUnmount(_ context.Context, env command.Envelope, started time.Time) command.Result {
	return failed(env, started, newReason(ReasonCodeUnimplemented, mountNotOurJob))
}

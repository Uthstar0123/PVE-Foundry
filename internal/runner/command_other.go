//go:build !linux

package runner

import (
	"context"
	"os/exec"
)

func (s *Service) command(ctx context.Context, work string, args ...string) *exec.Cmd {
	command := exec.CommandContext(ctx, s.cfg.OpenTofuBinary, args...)
	command.Dir = work
	command.Env = commandEnvironment()
	command.WaitDelay = 5_000_000_000
	return command
}

//go:build linux

package runner

import (
	"context"
	"os"
	"os/exec"
	"time"
)

func (s *Service) command(ctx context.Context, work string, args ...string) *exec.Cmd {
	if s.cfg.Environment != "production" {
		command := exec.CommandContext(ctx, s.cfg.OpenTofuBinary, args...)
		command.Dir = work
		command.Env = commandEnvironment()
		command.WaitDelay = 5 * time.Second
		return command
	}
	wrapped := []string{
		"--die-with-parent", "--new-session", "--unshare-user", "--unshare-pid", "--unshare-uts", "--unshare-ipc",
		"--proc", "/proc", "--dev", "/dev", "--tmpfs", "/tmp", "--setenv", "HOME", "/tmp",
		"--ro-bind", "/usr", "/usr", "--ro-bind", "/bin", "/bin", "--ro-bind", "/lib", "/lib",
		"--ro-bind", "/etc/resolv.conf", "/etc/resolv.conf", "--ro-bind", "/etc/hosts", "/etc/hosts",
		"--ro-bind", "/etc/ssl", "/etc/ssl", "--bind", work, work, "--bind", s.cfg.StateRoot, s.cfg.StateRoot, "--chdir", work,
	}
	if _, err := os.Stat("/lib64"); err == nil {
		wrapped = append(wrapped, "--ro-bind", "/lib64", "/lib64")
	}
	wrapped = append(wrapped, "--", s.cfg.OpenTofuBinary)
	wrapped = append(wrapped, args...)
	command := exec.CommandContext(ctx, "bwrap", wrapped...)
	command.Env = commandEnvironment()
	command.WaitDelay = 5 * time.Second
	return command
}

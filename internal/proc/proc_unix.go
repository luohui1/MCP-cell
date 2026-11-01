//go:build unix

package proc

import (
	"os/exec"
	"syscall"
)

type sysState struct{}

func start(cmd *exec.Cmd, _ uint64) (*sysState, error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &sysState{}, nil
}

func hideWindow(*exec.Cmd) {}

func (s *sysState) kill(cmd *exec.Cmd) {
	// The child leads its own group; kill the group so descendants go too.
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}

func (s *sysState) release() {}

func (s *sysState) memoryLimitHit() bool { return false }

func (s *sysState) usage(pid int) (Usage, error) { return groupUsage(pid) }

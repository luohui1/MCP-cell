//go:build unix

package proc

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

type sysState struct{}

func start(cmd *exec.Cmd) (*sysState, error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &sysState{}, nil
}

func (s *sysState) kill(cmd *exec.Cmd) {
	// The child leads its own group; kill the group so descendants go too.
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}

func (s *sysState) release() {}

func (s *sysState) usage(pid int) (Usage, error) { return statusOf(strconv.Itoa(pid)) }

// statusOf reads RssAnon (private resident memory) from /proc. Only the group
// leader is counted; full process-group accounting is M5 (cgroup v2).
func statusOf(pid string) (Usage, error) {
	f, err := os.Open("/proc/" + pid + "/status")
	if err != nil {
		return Usage{}, err
	}
	defer f.Close()
	u := Usage{Processes: 1}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		var kb uint64
		fmt.Sscanf(strings.TrimSpace(v), "%d", &kb)
		switch k {
		case "RssAnon":
			u.PrivateBytes = kb * 1024
		case "VmRSS":
			u.WorkingSetBytes = kb * 1024
		}
	}
	return u, nil
}

func Self() Usage {
	u, _ := statusOf("self")
	return u
}

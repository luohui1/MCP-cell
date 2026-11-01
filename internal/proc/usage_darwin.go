package proc

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// groupUsage adds up the resident memory of every process in pid's process
// group (an instance leads its own, so its children count too), from ps:
// macOS has no /proc, and its private memory needs libproc, which would take
// cgo. Resident memory includes shared pages, so it reads a little high.
func groupUsage(pid int) (Usage, error) {
	out, err := exec.Command("ps", "-A", "-o", "pid=,pgid=,rss=").Output()
	if err != nil {
		return Usage{}, err
	}
	var u Usage
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) != 3 || f[1] != strconv.Itoa(pid) {
			continue
		}
		kb, _ := strconv.ParseUint(f[2], 10, 64)
		u.Processes++
		u.PrivateBytes += kb * 1024
	}
	if u.Processes == 0 {
		return Usage{}, errors.New("进程不存在")
	}
	u.WorkingSetBytes = u.PrivateBytes
	return u, nil
}

// Self is this process's resident memory.
func Self() Usage { return processUsage(os.Getpid()) }

// processUsage is one process's resident memory.
func processUsage(pid int) Usage {
	out, err := exec.Command("ps", "-o", "rss=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return Usage{}
	}
	kb, _ := strconv.ParseUint(strings.TrimSpace(string(out)), 10, 64)
	return Usage{Processes: 1, PrivateBytes: kb * 1024, WorkingSetBytes: kb * 1024}
}

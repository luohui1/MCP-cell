package proc

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// groupUsage reads RssAnon (private resident memory) from /proc. Only the
// group leader is counted; full process-group accounting is M5 (cgroup v2).
func groupUsage(pid int) (Usage, error) { return statusOf(strconv.Itoa(pid)) }

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

func processUsage(pid int) Usage {
	u, _ := statusOf(strconv.Itoa(pid))
	return u
}

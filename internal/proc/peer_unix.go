//go:build unix

package proc

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// LoopbackPeer is not implemented on Unix: sessions are then counted per
// MCP instead of per agent process.
func LoopbackPeer(local, remote uint16) int { return 0 }

// Parent returns a process's parent and the parent's executable name, from
// /proc (Linux).
func Parent(pid int) (ppid int, exe string, err error) {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return 0, "", err
	}
	// pid (comm) state ppid ...; comm may contain spaces and parentheses.
	s := string(b)
	i := strings.LastIndexByte(s, ')')
	if i < 0 {
		return 0, "", errors.New("unreadable stat")
	}
	f := strings.Fields(s[i+1:])
	if len(f) < 2 {
		return 0, "", errors.New("unreadable stat")
	}
	ppid, err = strconv.Atoi(f[1])
	if err != nil {
		return 0, "", err
	}
	if target, err := os.Readlink("/proc/" + f[1] + "/exe"); err == nil {
		exe = filepath.Base(target)
	}
	return ppid, exe, nil
}

// Of reports one process's own memory (not its children's).
func Of(pid int) Usage { return processUsage(pid) }

package proc

import (
	"encoding/binary"
	"errors"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	iphlpapi                = windows.NewLazySystemDLL("iphlpapi.dll")
	procGetExtendedTcpTable = iphlpapi.NewProc("GetExtendedTcpTable")
)

// LoopbackPeer returns the process that owns the IPv4 loopback connection
// from port local to port remote (a client of the daemon), or 0.
func LoopbackPeer(local, remote uint16) int {
	const tcpTableOwnerPidConnections = 4 // TCP_TABLE_OWNER_PID_CONNECTIONS
	size := uint32(16 << 10)
	for range 4 {
		buf := make([]byte, size)
		r, _, _ := procGetExtendedTcpTable.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)),
			0, windows.AF_INET, tcpTableOwnerPidConnections, 0)
		if r == uintptr(windows.ERROR_INSUFFICIENT_BUFFER) {
			continue // size now holds what is needed
		}
		if r != 0 || len(buf) < 4 {
			return 0
		}
		// MIB_TCPTABLE_OWNER_PID: a count, then rows of six DWORDs: state,
		// local address, local port, remote address, remote port, owner.
		// Ports are in network byte order in the low 16 bits.
		n := int(binary.LittleEndian.Uint32(buf))
		for i := range n {
			row := buf[4+i*24:]
			if len(row) < 24 {
				break
			}
			lp := netPort(row[8:12])
			rp := netPort(row[16:20])
			if lp == local && rp == remote {
				return int(binary.LittleEndian.Uint32(row[20:24]))
			}
		}
		return 0
	}
	return 0
}

func netPort(b []byte) uint16 { return uint16(b[0])<<8 | uint16(b[1]) }

// Parent returns a process's parent and the parent's executable name.
func Parent(pid int) (ppid int, exe string, err error) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return 0, "", err
	}
	defer windows.CloseHandle(snap)
	names := map[uint32]string{}
	parent := uint32(0)
	found := false
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		names[e.ProcessID] = windows.UTF16ToString(e.ExeFile[:])
		if e.ProcessID == uint32(pid) {
			parent, found = e.ParentProcessID, true
		}
	}
	if !found {
		return 0, "", errors.New("no such process")
	}
	return int(parent), strings.ToLower(names[parent]), nil
}

// Of reports one process's own memory (not its children's).
func Of(pid int) Usage {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return Usage{}
	}
	defer windows.CloseHandle(h)
	c, err := memoryOf(h)
	if err != nil {
		return Usage{}
	}
	return Usage{Processes: 1, PrivateBytes: uint64(c.PrivateUsage), WorkingSetBytes: uint64(c.WorkingSetSize)}
}

package proc

import (
	"errors"
	"fmt"
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	ntdll                = windows.NewLazySystemDLL("ntdll.dll")
	procNtResumeProcess  = ntdll.NewProc("NtResumeProcess")
	kernel32             = windows.NewLazySystemDLL("kernel32.dll")
	procGetProcessMemory = kernel32.NewProc("K32GetProcessMemoryInfo")
)

type sysState struct{ job windows.Handle }

func start(cmd *exec.Cmd) (*sysState, error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		// Suspended until it is in the job; CREATE_NO_WINDOW gives the tree a
		// hidden console so npx/cmd children do not flash windows.
		CreationFlags: windows.CREATE_SUSPENDED | windows.CREATE_NO_WINDOW,
		HideWindow:    true,
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	s, err := adopt(uint32(cmd.Process.Pid))
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, fmt.Errorf("Windows 进程归属建立失败，未运行该服务：%w", err)
	}
	return s, nil
}

func adopt(pid uint32) (*sysState, error) {
	const access = windows.PROCESS_SET_QUOTA | windows.PROCESS_TERMINATE |
		windows.PROCESS_SUSPEND_RESUME | windows.PROCESS_QUERY_LIMITED_INFORMATION
	h, err := windows.OpenProcess(access, false, pid)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(h)
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		windows.CloseHandle(job)
		return nil, err
	}
	if err := windows.AssignProcessToJobObject(job, h); err != nil {
		windows.CloseHandle(job)
		return nil, err
	}
	if st, _, _ := procNtResumeProcess.Call(uintptr(h)); st != 0 {
		windows.CloseHandle(job) // KILL_ON_JOB_CLOSE ends the suspended child
		return nil, fmt.Errorf("NtResumeProcess: 0x%x", st)
	}
	return &sysState{job: job}, nil
}

func (s *sysState) kill(*exec.Cmd) {
	if s.job != 0 {
		_ = windows.TerminateJobObject(s.job, 1)
	}
}

func (s *sysState) release() {
	if s.job != 0 {
		windows.CloseHandle(s.job)
		s.job = 0
	}
}

// processMemoryCountersEx is PROCESS_MEMORY_COUNTERS_EX.
type processMemoryCountersEx struct {
	CB                         uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
	PrivateUsage               uintptr
}

func memoryOf(h windows.Handle) (processMemoryCountersEx, error) {
	var c processMemoryCountersEx
	c.CB = uint32(unsafe.Sizeof(c))
	if ok, _, err := procGetProcessMemory.Call(uintptr(h), uintptr(unsafe.Pointer(&c)), uintptr(c.CB)); ok == 0 {
		return c, err
	}
	return c, nil
}

const maxJobPids = 512

func (s *sysState) usage(int) (Usage, error) {
	if s.job == 0 {
		return Usage{}, errors.New("job closed")
	}
	// JOBOBJECT_BASIC_PROCESS_ID_LIST: two DWORD counts, then ULONG_PTR ids.
	const header = 8
	buf := make([]byte, header+maxJobPids*unsafe.Sizeof(uintptr(0)))
	err := windows.QueryInformationJobObject(s.job, windows.JobObjectBasicProcessIdList,
		uintptr(unsafe.Pointer(&buf[0])), uint32(len(buf)), nil)
	if err != nil && !errors.Is(err, windows.ERROR_MORE_DATA) {
		return Usage{}, err
	}
	n := *(*uint32)(unsafe.Pointer(&buf[4]))
	var u Usage
	for i := uint32(0); i < n && i < maxJobPids; i++ {
		pid := *(*uintptr)(unsafe.Pointer(&buf[header+uintptr(i)*unsafe.Sizeof(uintptr(0))]))
		h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
		if err != nil {
			continue
		}
		if c, err := memoryOf(h); err == nil {
			u.Processes++
			u.PrivateBytes += uint64(c.PrivateUsage)
			u.WorkingSetBytes += uint64(c.WorkingSetSize)
		}
		windows.CloseHandle(h)
	}
	return u, nil
}

// Self reports the calling process's own memory (the daemon).
func Self() Usage {
	c, err := memoryOf(windows.CurrentProcess())
	if err != nil {
		return Usage{}
	}
	return Usage{Processes: 1, PrivateBytes: uint64(c.PrivateUsage), WorkingSetBytes: uint64(c.WorkingSetSize)}
}

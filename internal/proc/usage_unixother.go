//go:build unix && !linux && !darwin

package proc

import "errors"

// groupUsage is not measured on this system.
func groupUsage(int) (Usage, error) { return Usage{}, errors.New("这个系统还不能统计内存") }

func Self() Usage { return Usage{} }

func processUsage(int) Usage { return Usage{} }

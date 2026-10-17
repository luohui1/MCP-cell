// Package proc starts an upstream MCP server as an owned process tree.
//
// On Windows the child is created suspended, put in a Job Object with
// KILL_ON_JOB_CLOSE, then resumed, so every descendant (cmd.exe, npx, node...)
// is in the job before it can run, and the whole tree dies with the daemon.
// On Unix the child leads its own process group.
package proc

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
)

type Spec struct {
	Argv []string
	Env  []string // KEY=VALUE, exactly what the child sees
	Dir  string
}

// Usage is memory for the whole owned tree. PrivateBytes is private commit on
// Windows (what counts against the commit limit even after the working set is
// trimmed); on Linux it is the leader's anonymous RSS.
type Usage struct {
	Processes       int    `json:"processes"`
	PrivateBytes    uint64 `json:"privateBytes"`
	WorkingSetBytes uint64 `json:"workingSetBytes"`
}

type Process struct {
	Pid    int
	Argv   []string // after resolution, for diagnostics
	Stdin  io.WriteCloser
	Stdout io.ReadCloser
	Stderr io.ReadCloser

	cmd  *exec.Cmd
	sys  *sysState
	done chan struct{}
	err  error
	once sync.Once
}

// Start resolves spec.Argv and launches it with fresh pipes. The parent keeps
// only its own pipe ends, so Stdout reaches EOF once every process holding the
// child end (including grandchildren) has exited.
func Start(spec Spec) (*Process, error) {
	argv, err := ResolveArgv(spec.Argv, spec.Env, spec.Dir)
	if err != nil {
		return nil, err
	}
	inR, inW, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		closeAll(inR, inW)
		return nil, err
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		closeAll(inR, inW, outR, outW)
		return nil, err
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env, cmd.Dir = spec.Env, spec.Dir
	cmd.Stdin, cmd.Stdout, cmd.Stderr = inR, outW, errW
	sys, err := start(cmd)
	closeAll(inR, outW, errW) // the child has its own copies now
	if err != nil {
		closeAll(inW, outR, errR)
		return nil, err
	}
	p := &Process{Pid: cmd.Process.Pid, Argv: argv, Stdin: inW, Stdout: outR, Stderr: errR,
		cmd: cmd, sys: sys, done: make(chan struct{})}
	go func() {
		p.err = cmd.Wait()
		close(p.done)
	}()
	return p, nil
}

// Done is closed when the direct child has exited. Descendants may outlive it
// until Kill.
func (p *Process) Done() <-chan struct{} { return p.done }

func (p *Process) ExitErr() error {
	select {
	case <-p.done:
		return p.err
	default:
		return nil
	}
}

// Kill terminates the whole tree and releases the parent's pipe ends.
// Safe to call more than once.
func (p *Process) Kill() {
	p.once.Do(func() {
		p.sys.kill(p.cmd)
		closeAll(p.Stdin)
		<-p.done
		p.sys.release()
		closeAll(p.Stdout, p.Stderr)
	})
}

func (p *Process) Usage() (Usage, error) {
	select {
	case <-p.done:
		return Usage{}, errors.New("process exited")
	default:
	}
	return p.sys.usage(p.Pid)
}

func closeAll(cs ...io.Closer) {
	for _, c := range cs {
		if c != nil {
			_ = c.Close()
		}
	}
}

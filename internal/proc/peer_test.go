package proc

import (
	"net"
	"os"
	"runtime"
	"testing"
)

func TestLoopbackPeer(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("only looked up on Windows")
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	c, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	s, err := l.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	// What the daemon sees: the peer's port and its own.
	peer := uint16(s.RemoteAddr().(*net.TCPAddr).Port)
	own := uint16(s.LocalAddr().(*net.TCPAddr).Port)
	if got := LoopbackPeer(peer, own); got != os.Getpid() {
		t.Fatalf("LoopbackPeer = %d, want this process %d", got, os.Getpid())
	}
	if got := LoopbackPeer(own, peer); got != os.Getpid() {
		t.Fatalf("the listening side's row: %d", got)
	}
	if got := LoopbackPeer(1, 2); got != 0 {
		t.Fatalf("no such connection: %d", got)
	}
}

func TestParent(t *testing.T) {
	if runtime.GOOS != "windows" && runtime.GOOS != "linux" {
		t.Skip("needs /proc")
	}
	ppid, _, err := Parent(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if ppid != os.Getppid() {
		t.Fatalf("Parent = %d, want %d", ppid, os.Getppid())
	}
	if Of(os.Getpid()).PrivateBytes == 0 {
		t.Fatal("Of(self) reported no memory")
	}
}

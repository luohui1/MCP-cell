package desktop

import (
	"bytes"
	"image/png"
	"os"
	"testing"
)

func TestPickIcon(t *testing.T) {
	ico, err := os.ReadFile("mcpcell.ico")
	if err != nil {
		t.Fatal(err)
	}
	for size, want := range map[int]int{16: 16, 18: 20, 30: 32, 32: 32, 100: 256, 300: 256} {
		b, err := pickIcon(ico, size)
		if err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(bytes.NewReader(b))
		if err != nil {
			t.Fatalf("%d: %v", size, err)
		}
		if got := img.Bounds().Dx(); got != want {
			t.Errorf("pickIcon(%d) = %d px, want %d", size, got, want)
		}
	}
	if _, err := pickIcon([]byte("nope"), 16); err == nil {
		t.Error("garbage accepted")
	}
}

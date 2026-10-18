// Command mkicon renders the MCPcell logo (the Logo SVG in web/src/icons.tsx)
// with headless Edge and packs it into a Windows .ico with PNG entries.
//
//	go run ./tools/mkicon            # writes internal/desktop/mcpcell.ico
//
// Small sizes use slightly bolder strokes so the nodes stay visible at 16 px.
package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var sizes = []int{16, 20, 24, 32, 40, 48, 64, 256}

const gap = 12

// logo is the 32×32 design from icons.tsx, cropped to the hexagon so it
// fills the icon canvas. Bold is used for sizes up to 24 px.
func logo(size int, bold bool) string {
	stroke, node, hub := "1.8", "1.9", "3.6"
	if bold {
		stroke, node, hub = "2.6", "2.5", "4.2"
	}
	return fmt.Sprintf(`<svg width="%d" height="%d" viewBox="1.5 1.5 29 29" xmlns="http://www.w3.org/2000/svg">`+
		`<defs><linearGradient id="g%d" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="#ff9b45"/><stop offset="1" stop-color="#ef5f0f"/></linearGradient></defs>`+
		`<path d="M16 2.2 28 9.1v13.8L16 29.8 4 22.9V9.1Z" fill="url(#g%d)"/>`+
		`<path d="M16 16V8.2M16 16l6.8 3.9M16 16l-6.8 3.9" stroke="#fff" stroke-width="%s" stroke-linecap="round" opacity=".9"/>`+
		`<circle cx="16" cy="16" r="%s" fill="#fff"/><circle cx="16" cy="8.2" r="%s" fill="#fff"/>`+
		`<circle cx="22.8" cy="19.9" r="%s" fill="#fff"/><circle cx="9.2" cy="19.9" r="%s" fill="#fff"/></svg>`,
		size, size, size, size, stroke, hub, node, node, node)
}

func main() {
	edge := flag.String("edge", `C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`, "msedge.exe")
	out := flag.String("out", filepath.Join("internal", "desktop", "mcpcell.ico"), "output .ico")
	flag.Parse()

	dir, err := os.MkdirTemp("", "mkicon")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)

	var page strings.Builder
	page.WriteString(`<!doctype html><html style="background:transparent"><body style="margin:0;background:transparent">`)
	x := 0
	for _, s := range sizes {
		fmt.Fprintf(&page, `<div style="position:absolute;left:%dpx;top:0;width:%dpx;height:%dpx">%s</div>`, x, s, s, logo(s, s <= 24))
		x += s + gap
	}
	page.WriteString(`</body></html>`)
	html := filepath.Join(dir, "logo.html")
	shot := filepath.Join(dir, "logo.png")
	if err := os.WriteFile(html, []byte(page.String()), 0o600); err != nil {
		log.Fatal(err)
	}
	cmd := exec.Command(*edge, "--headless=new", "--disable-gpu", "--hide-scrollbars", "--force-device-scale-factor=1",
		"--default-background-color=00000000", "--user-data-dir="+filepath.Join(dir, "profile"),
		fmt.Sprintf("--window-size=%d,%d", x+gap, 300), "--screenshot="+shot, "file:///"+filepath.ToSlash(html))
	if b, err := cmd.CombinedOutput(); err != nil {
		log.Fatalf("edge: %v\n%s", err, b)
	}
	f, err := os.Open(shot)
	if err != nil {
		log.Fatal(err)
	}
	full, err := png.Decode(f)
	f.Close()
	if err != nil {
		log.Fatal(err)
	}

	var images [][]byte
	x = 0
	for _, s := range sizes {
		img := image.NewNRGBA(image.Rect(0, 0, s, s))
		draw.Draw(img, img.Bounds(), full, image.Pt(x, 0), draw.Src)
		if _, _, _, a := img.At(0, 0).RGBA(); a != 0 {
			log.Fatalf("%d px: corner is not transparent; Edge ignored --default-background-color", s)
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			log.Fatal(err)
		}
		images = append(images, buf.Bytes())
		x += s + gap
	}

	var ico bytes.Buffer
	le := binary.LittleEndian
	_ = binary.Write(&ico, le, [3]uint16{0, 1, uint16(len(sizes))}) // ICONDIR
	offset := 6 + 16*len(sizes)
	for i, s := range sizes {
		dim := byte(s)
		if s >= 256 {
			dim = 0
		}
		_ = binary.Write(&ico, le, struct {
			W, H, Colors, Reserved byte
			Planes, Bits           uint16
			Size, Offset           uint32
		}{dim, dim, 0, 0, 1, 32, uint32(len(images[i])), uint32(offset)})
		offset += len(images[i])
	}
	for _, b := range images {
		ico.Write(b)
	}
	if err := os.WriteFile(*out, ico.Bytes(), 0o644); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%s: %d sizes, %d bytes\n", *out, len(sizes), ico.Len())
}

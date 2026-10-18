package desktop

import (
	"encoding/binary"
	"errors"
)

// pickIcon returns the image bytes of the .ico entry that best fits size:
// an exact match, else the smallest larger one, else the largest.
func pickIcon(ico []byte, size int) ([]byte, error) {
	le := binary.LittleEndian
	if len(ico) < 6 || le.Uint16(ico[2:]) != 1 {
		return nil, errors.New("not an .ico file")
	}
	n := int(le.Uint16(ico[4:]))
	best, bestDim := -1, 0
	for i := range n {
		e := 6 + 16*i
		if e+16 > len(ico) {
			return nil, errors.New("truncated .ico directory")
		}
		dim := int(ico[e])
		if dim == 0 {
			dim = 256
		}
		better := best < 0 ||
			(dim >= size && (bestDim < size || dim < bestDim)) ||
			(dim < size && bestDim < size && dim > bestDim)
		if better {
			best, bestDim = i, dim
		}
	}
	if best < 0 {
		return nil, errors.New("empty .ico file")
	}
	e := 6 + 16*best
	size32, off := int(le.Uint32(ico[e+8:])), int(le.Uint32(ico[e+12:]))
	if off < 0 || size32 <= 0 || off+size32 > len(ico) {
		return nil, errors.New("truncated .ico image")
	}
	return ico[off : off+size32], nil
}

package skills

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"unicode/utf16"

	"golang.org/x/sys/windows"
)

// makeLink points link at the directory target. On Windows it makes a
// directory junction: unlike a symbolic link it needs neither administrator
// rights nor Developer Mode, and every agent follows it like a symlink.
func makeLink(target, link string) (junction bool, err error) {
	return true, makeJunction(target, link)
}

// makeSymlink recreates a symbolic link found before a sync, for restore.
func makeSymlink(target, link string) error { return os.Symlink(target, link) }

func makeJunction(target, link string) error {
	abs, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	if err := os.Mkdir(link, 0o755); err != nil {
		return err
	}
	if err := setMountPoint(link, abs); err != nil {
		_ = os.Remove(link)
		return fmt.Errorf("无法创建目录链接：%w", err)
	}
	return nil
}

// setMountPoint writes a REPARSE_DATA_BUFFER with IO_REPARSE_TAG_MOUNT_POINT
// onto the empty directory dir.
func setMountPoint(dir, target string) error {
	sub := utf16.Encode([]rune(`\??\` + target))
	prn := utf16.Encode([]rune(target))
	// PathBuffer holds both names, each NUL-terminated.
	pathBytes := (len(sub) + 1 + len(prn) + 1) * 2
	dataLen := 8 + pathBytes // the four uint16 offsets/lengths, then the names
	buf := make([]byte, 8+dataLen)
	le := binary.LittleEndian
	le.PutUint32(buf[0:], windows.IO_REPARSE_TAG_MOUNT_POINT)
	le.PutUint16(buf[4:], uint16(dataLen))
	le.PutUint16(buf[8:], 0)                       // SubstituteNameOffset
	le.PutUint16(buf[10:], uint16(len(sub)*2))     // SubstituteNameLength
	le.PutUint16(buf[12:], uint16((len(sub)+1)*2)) // PrintNameOffset
	le.PutUint16(buf[14:], uint16(len(prn)*2))     // PrintNameLength
	off := 16
	for _, c := range sub {
		le.PutUint16(buf[off:], c)
		off += 2
	}
	off += 2
	for _, c := range prn {
		le.PutUint16(buf[off:], c)
		off += 2
	}

	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return err
	}
	h, err := windows.CreateFile(p, windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	var n uint32
	return windows.DeviceIoControl(h, windows.FSCTL_SET_REPARSE_POINT, &buf[0], uint32(len(buf)), nil, 0, &n, nil)
}

// isJunction reports whether fi (from Lstat) is a junction rather than a
// symbolic link. Go reports junctions as irregular files, not symlinks.
func isJunction(fi os.FileInfo) bool {
	return fi.Mode()&os.ModeIrregular != 0 && fi.Mode()&os.ModeSymlink == 0
}

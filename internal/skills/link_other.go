//go:build !windows

package skills

import "os"

// makeLink points link at the directory target with a symbolic link.
func makeLink(target, link string) (junction bool, err error) {
	return false, os.Symlink(target, link)
}

func makeSymlink(target, link string) error { return os.Symlink(target, link) }

func makeJunction(target, link string) error { return os.Symlink(target, link) }

func isJunction(os.FileInfo) bool { return false }

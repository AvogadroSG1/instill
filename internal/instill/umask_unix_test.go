//go:build !windows

package instill

import "syscall"

func setTestUmask(mask int) bool {
	syscall.Umask(mask)
	return true
}

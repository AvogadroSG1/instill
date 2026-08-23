//go:build windows

package instill

func setTestUmask(int) bool {
	return false
}

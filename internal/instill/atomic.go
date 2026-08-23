package instill

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
)

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	emitMutationTestEvent("first-write:" + path)
	dir := filepath.Dir(path)
	file, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}

	tmp := file.Name()
	defer func() {
		_ = os.Remove(tmp)
	}()

	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Chmod(perm); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}

	return os.Rename(tmp, path)
}

func writeNewFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	var file *os.File
	var err error
	for range 10 {
		random := make([]byte, 8)
		if _, err = rand.Read(random); err != nil {
			return err
		}
		tmp := filepath.Join(dir, filepath.Base(path)+"."+hex.EncodeToString(random)+".tmp")
		file, err = os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm) //nolint:gosec // The process umask intentionally restricts the requested manifest mode.
		if err == nil {
			break
		}
		if !os.IsExist(err) {
			return err
		}
	}
	if file == nil {
		return err
	}

	tmp := file.Name()
	defer func() {
		_ = os.Remove(tmp)
	}()
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

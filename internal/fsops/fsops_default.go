//go:build !windows

package fsops

import "os"

func MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

func WriteFile(path string, data []byte, perm os.FileMode) error {
	return os.WriteFile(path, data, perm)
}

func Remove(path string) error {
	return os.Remove(path)
}

func RemoveAll(path string) error {
	return os.RemoveAll(path)
}
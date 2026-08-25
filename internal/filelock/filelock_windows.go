//go:build windows

package filelock

import "errors"

func With(_ string, _ func() error) error {
	return errors.New("cross-process durability locks are not implemented on Windows in v0")
}

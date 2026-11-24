//go:build !windows

// Copyright 2026 National Library of Norway.
// SPDX-License-Identifier: Apache-2.0

package gowarc

import (
	"errors"
	"os"
	"path/filepath"
)

func rename(from, to string) error {
	if err := os.Rename(from, to); err != nil {
		return err
	}

	dir, err := os.Open(filepath.Dir(to))
	if err != nil {
		return err
	}
	return errors.Join(dir.Sync(), dir.Close())
}

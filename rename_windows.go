// Copyright 2026 National Library of Norway.
// SPDX-License-Identifier: Apache-2.0

package gowarc

import (
	"strings"

	"golang.org/x/sys/windows"
)

func rename(from, to string) error {
	source, err := movePath(from)
	if err != nil {
		return err
	}
	destination, err := movePath(to)
	if err != nil {
		return err
	}
	// Windows rejects Sync on a directory handle opened by os.Open.
	return windows.MoveFileEx(source, destination, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}

func movePath(name string) (*uint16, error) {
	name, err := windows.FullPath(name)
	if err != nil {
		return nil, err
	}
	// Preserve long-path support even when the application's manifest does not opt in.
	if len(name) >= windows.MAX_PATH && !strings.HasPrefix(name, `\\?\`) && !strings.HasPrefix(name, `\\.\`) {
		if share, ok := strings.CutPrefix(name, `\\`); ok {
			name = `\\?\UNC\` + share
		} else {
			name = `\\?\` + name
		}
	}
	return windows.UTF16PtrFromString(name)
}

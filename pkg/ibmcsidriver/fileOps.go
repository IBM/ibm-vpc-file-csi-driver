/**
 *
 * Copyright 2024- IBM Inc. All rights reserved
 * SPDX-License-Identifier: Apache2.0
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

// Package ibmcsidriver ...
package ibmcsidriver

import (
	"os"
	"strconv"
)

const (
	filePermission = 0660
)

// socketPermission represents file system operations
type socketPermission interface {
	chown(name string, uid, gid int) error
	chmod(name string, mode os.FileMode) error
}

// realSocketPermission implements socketPermission
type opsSocketPermission struct{}

func (f *opsSocketPermission) chown(name string, uid, gid int) error {
	return os.Chown(name, uid, gid)
}

func (f *opsSocketPermission) chmod(name string, mode os.FileMode) error {
	return os.Chmod(name, mode)
}

// setupSidecar updates owner/group and permission of the file given(addr)
func setupSidecar(addr string, ops socketPermission) error {
	groupSt := os.Getenv("SIDECAR_GROUP_ID")
	// If env is not set, set default to 0
	if groupSt == "" {
		groupSt = "0"
	}

	group, err := strconv.Atoi(groupSt)
	if err != nil {
		return err
	}

	// Change group of csi socket to non-root user for enabling the csi sidecar
	if err := ops.chown(addr, -1, group); err != nil {
		return err
	}

	// Modify permissions of csi socket
	// Only the users and the group owners will have read/write access to csi socket
	if err := ops.chmod(addr, filePermission); err != nil {
		return err
	}

	return nil
}

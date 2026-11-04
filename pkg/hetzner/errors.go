/*
 * Copyright 2023 Simon Emms <simon@simonemms.com>
 * Copyright 2026 github.com/FKint/devpod-provider-hetzner Authors
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

package hetzner

import (
	"errors"
	"fmt"
)

var (
	ErrProvisioningFailed   = errors.New("server provisioning failed")
	ErrServerBusy           = errors.New("server is busy")
	ErrDiskTooSmall         = errors.New("disk size too small")
	ErrVolumeAttached       = errors.New("volume is attached to another server")
	ErrSSHKeyInUse          = errors.New("ssh key is used by another machine")
	ErrSwapTooLarge         = errors.New("swap does not fit on the server's local disk")
	errSSHAuth              = errors.New("ssh authentication failed")
	errUnreachable          = errors.New("ssh server unreachable")
	ErrMultipleServersFound = func(name string) error {
		return fmt.Errorf("multiple server with name %s found", name)
	}
	ErrMultipleVolumesFound = func(name string) error {
		return fmt.Errorf("multiple volumes with name %s found", name)
	}
	ErrUnknownDiskImage   = errors.New("unknown disk image")
	ErrUnknownMachineType = errors.New("unknown machine type")
	ErrUnknownRegion      = errors.New("unknown region")
)

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

import "time"

const (
	labelMachineID = "machineId"
	SSHUsername    = "devpod"
	SSHPort        = 22

	// maxSSHKeyNameIDLength is how much of the machine ID is used in an SSH key name.
	maxSSHKeyNameIDLength = 24

	// actionTimeout bounds how long we wait for Hetzner actions to complete.
	// Server creation with attached volumes can exceed a minute on a busy API.
	actionTimeout = 5 * time.Minute
	// provisionTimeout bounds how long we wait for cloud-init (incl. the Docker
	// install) to finish on a new server.
	provisionTimeout = 10 * time.Minute
	provisionPoll    = 2 * time.Second
	// cloudInitErrorThreshold is how many consecutive "error" reports count as a failure.
	cloudInitErrorThreshold = 3
	sshDialTimeout          = 5 * time.Second
	authFailureThreshold    = 5
	sshAttemptTimeout       = time.Minute
	// statusTimeout bounds how long the CLI waits for a machine to reach a status.
	statusTimeout = 5 * time.Minute
	statusPoll    = time.Second
	// swapDiskReserveGB is the local disk space kept free of swap for the OS and Docker's runtime files.
	swapDiskReserveGB = 4
	// diskWarnPercent is the volume usage from which provisioning warns.
	diskWarnPercent = 85
)

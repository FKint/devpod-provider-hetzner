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

package options

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// MinDiskSizeGB is the smallest volume Hetzner allows. It only applies when a
// volume is created; an existing volume is only ever grown to DISK_SIZE.
const MinDiskSizeGB = 10

type Options struct {
	MachineID     string
	MachineFolder string

	Region      string
	DiskImage   string
	DiskSize    string
	MachineType string
	Token       string
}

func FromEnv(skipMachine bool) (*Options, error) {
	o := &Options{}

	type option struct {
		name string
		dest *string
	}

	required := []option{
		{"HCLOUD_TOKEN", &o.Token},
		{"DISK_SIZE", &o.DiskSize},
		{"DISK_IMAGE", &o.DiskImage},
		{"MACHINE_TYPE", &o.MachineType},
		{"REGION", &o.Region},
	}
	if !skipMachine {
		required = append([]option{{"MACHINE_ID", &o.MachineID}, {"MACHINE_FOLDER", &o.MachineFolder}}, required...)
	}

	for _, r := range required {
		val, err := fromEnvOrError(r.name)
		if err != nil {
			return nil, err
		}
		*r.dest = val
	}

	return o, nil
}

// DiskSizeGB parses the DISK_SIZE option.
func (o *Options) DiskSizeGB() (int, error) {
	size, err := strconv.Atoi(strings.TrimSpace(o.DiskSize))
	if err != nil {
		return 0, fmt.Errorf("parse disk size %q: %w", o.DiskSize, err)
	}
	if size <= 0 {
		return 0, fmt.Errorf("disk size must be positive, got %d", size)
	}

	return size, nil
}

func fromEnvOrError(name string) (string, error) {
	if val := os.Getenv(name); val != "" {
		return val, nil
	}

	return "", fmt.Errorf("couldn't find option %s in environment, please make sure it is defined", name)
}

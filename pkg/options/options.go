/*
 * Copyright 2023 Simon Emms <simon@simonemms.com>
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
	retOptions := &Options{}

	var err error
	if !skipMachine {
		retOptions.MachineID, err = fromEnvOrError("MACHINE_ID")
		if err != nil {
			return nil, err
		}

		retOptions.MachineFolder, err = fromEnvOrError("MACHINE_FOLDER")
		if err != nil {
			return nil, err
		}
	}

	// TOKEN is deprecated
	retOptions.Token, err = fromEnvOrError("HCLOUD_TOKEN", "TOKEN")
	if err != nil {
		return nil, err
	}
	retOptions.DiskSize, err = fromEnvOrError("DISK_SIZE")
	if err != nil {
		return nil, err
	}
	retOptions.DiskImage, err = fromEnvOrError("DISK_IMAGE")
	if err != nil {
		return nil, err
	}
	retOptions.MachineType, err = fromEnvOrError("MACHINE_TYPE")
	if err != nil {
		return nil, err
	}
	retOptions.Region, err = fromEnvOrError("REGION")
	if err != nil {
		return nil, err
	}

	return retOptions, nil
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

func fromEnvOrError(name string, fallback ...string) (string, error) {
	envvars := append([]string{name}, fallback...)

	for _, e := range envvars {
		val := os.Getenv(e)
		if val != "" {
			return val, nil
		}
	}

	envvarCsv := strings.Join(envvars, ", ")

	return "", fmt.Errorf("couldn't find option %s in environment, please make sure %s is defined", envvarCsv, envvarCsv)
}

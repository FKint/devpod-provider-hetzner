/*
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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	machineEnvNames = []string{"MACHINE_ID", "MACHINE_FOLDER"}
	commonEnvNames  = []string{"HCLOUD_TOKEN", "DISK_SIZE", "DISK_IMAGE", "MACHINE_TYPE", "REGION"}
)

func setEnv(t *testing.T, names []string) {
	t.Helper()
	for _, n := range names {
		t.Setenv(n, "value-of-"+n)
	}
}

func TestFromEnv(t *testing.T) {
	setEnv(t, machineEnvNames)
	setEnv(t, commonEnvNames)

	o, err := FromEnv(false)
	require.NoError(t, err)
	assert.Equal(t, "value-of-MACHINE_ID", o.MachineID)
	assert.Equal(t, "value-of-MACHINE_FOLDER", o.MachineFolder)
	assert.Equal(t, "value-of-REGION", o.Region)
	assert.Equal(t, "value-of-DISK_IMAGE", o.DiskImage)
	assert.Equal(t, "value-of-DISK_SIZE", o.DiskSize)
	assert.Equal(t, "value-of-MACHINE_TYPE", o.MachineType)
	assert.Equal(t, "value-of-HCLOUD_TOKEN", o.Token)
}

// The init command runs before there is a machine, so it must not need one
func TestFromEnvSkippingTheMachine(t *testing.T) {
	setEnv(t, commonEnvNames)
	t.Setenv("MACHINE_ID", "")
	t.Setenv("MACHINE_FOLDER", "")

	o, err := FromEnv(true)
	require.NoError(t, err)
	assert.Equal(t, "value-of-HCLOUD_TOKEN", o.Token)
	assert.Empty(t, o.MachineID)

	_, err = FromEnv(false)
	assert.Error(t, err, "a command that works on a machine needs its ID")
}

func TestFromEnvNamesTheMissingOption(t *testing.T) {
	for _, missing := range append(append([]string{}, machineEnvNames...), commonEnvNames...) {
		t.Run(missing, func(t *testing.T) {
			setEnv(t, machineEnvNames)
			setEnv(t, commonEnvNames)
			t.Setenv(missing, "")

			o, err := FromEnv(false)
			assert.Nil(t, o)
			assert.ErrorContains(t, err, missing)
		})
	}
}

func TestFromEnvSkippingTheMachineStillNeedsTheRest(t *testing.T) {
	for _, missing := range commonEnvNames {
		setEnv(t, commonEnvNames)
		t.Setenv(missing, "")

		_, err := FromEnv(true)
		assert.ErrorContains(t, err, missing)
	}
}

func TestFromEnvOrError(t *testing.T) {
	t.Setenv("SOME_OPTION", "x")
	got, err := fromEnvOrError("SOME_OPTION")
	assert.NoError(t, err)
	assert.Equal(t, "x", got)

	t.Setenv("SOME_OPTION", "")
	_, err = fromEnvOrError("SOME_OPTION")
	assert.ErrorContains(t, err, "SOME_OPTION")
}

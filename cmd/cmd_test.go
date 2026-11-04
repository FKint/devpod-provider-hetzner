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

package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// setProviderEnv sets a complete, valid provider environment. The token is bogus and
// the tests below must fail on validation, before anything reaches the Hetzner API.
const (
	envToken     = "HCLOUD_TOKEN"
	envMachineID = "MACHINE_ID"
	envDiskSize  = "DISK_SIZE"
)

func setProviderEnv(t *testing.T, overrides map[string]string) {
	t.Helper()

	env := map[string]string{
		envToken:         "not-a-real-token",
		envMachineID:     "test-machine",
		"MACHINE_FOLDER": t.TempDir(),
		envDiskSize:      "10",
		"DISK_IMAGE":     "docker-ce",
		"MACHINE_TYPE":   "cx23",
		"REGION":         "nbg1",
	}
	for k, v := range overrides {
		env[k] = v
	}
	for k, v := range env {
		t.Setenv(k, v)
	}
}

func TestCreateValidatesBeforeCallingTheAPI(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"missing token", map[string]string{envToken: ""}, envToken},
		{"missing machine ID", map[string]string{envMachineID: ""}, envMachineID},
		{"disk size is not a number", map[string]string{envDiskSize: "ten"}, "disk size"},
		{"disk size is not positive", map[string]string{envDiskSize: "0"}, "disk size"},
		{"invalid machine option", map[string]string{"SWAP_SIZE": "lots"}, "SWAP_SIZE"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setProviderEnv(t, tt.env)
			assert.ErrorContains(t, createOrStartServer(createCmd, nil), tt.want)
		})
	}
}

// Every command that works on a machine reads the environment first
func TestMachineCommandsNeedTheProviderEnvironment(t *testing.T) {
	setProviderEnv(t, map[string]string{envMachineID: ""})
	assert.ErrorContains(t, statusCmd.RunE(statusCmd, nil), envMachineID)
	assert.ErrorContains(t, stopCmd.RunE(stopCmd, nil), envMachineID)
	assert.ErrorContains(t, startCmd.RunE(startCmd, nil), envMachineID)
	assert.ErrorContains(t, deleteCmd.RunE(deleteCmd, nil), envMachineID)
}

func TestCommandsAreRegistered(t *testing.T) {
	var names []string
	for _, c := range rootCmd.Commands() {
		names = append(names, c.Name())
	}
	assert.ElementsMatch(t, []string{"command", "create", "delete", "init", "start", "status", "stop", "version"}, names)
}

func TestStopHasAFromMachineFlag(t *testing.T) {
	flag := stopCmd.Flags().Lookup("from-machine")
	if assert.NotNil(t, flag) {
		assert.Equal(t, "false", flag.DefValue, "a stop from the client must not skip the ACPI shutdown")
	}
}

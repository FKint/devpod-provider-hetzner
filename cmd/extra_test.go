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
	"context"
	"testing"

	"github.com/FKint/devpod-provider-hetzner/pkg/hetzner"
	"github.com/loft-sh/devpod/pkg/client"
	"github.com/stretchr/testify/assert"
)

func TestRunAndWaitNeedsTheProviderEnvironment(t *testing.T) {
	setProviderEnv(t, map[string]string{envToken: ""})

	called := false
	err := runAndWait(func(*hetzner.Hetzner, context.Context, string) error {
		called = true
		return nil
	}, client.StatusRunning)

	assert.ErrorContains(t, err, envToken)
	assert.False(t, called, "the action must not run when the environment is invalid")
}

func TestInitNeedsAToken(t *testing.T) {
	setProviderEnv(t, map[string]string{envToken: ""})
	assert.ErrorContains(t, initCmd.RunE(initCmd, nil), envToken)
}

func TestCommandValidatesBeforeCallingTheAPI(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		cmd  string
		want string
	}{
		{"missing token", map[string]string{envToken: ""}, "ls", envToken},
		{"missing command", nil, "", "command"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setProviderEnv(t, tt.env)
			t.Setenv("COMMAND", tt.cmd)
			assert.ErrorContains(t, commandCmd.RunE(commandCmd, nil), tt.want)
		})
	}
}

func TestRootPreRunHandlesLogLevels(t *testing.T) {
	for _, level := range []string{"", "debug", "not-a-level"} {
		t.Run("level "+level, func(t *testing.T) {
			t.Setenv("DEVPOD_LOG_LEVEL", level)
			assert.NoError(t, rootCmd.PersistentPreRunE(rootCmd, nil))
		})
	}
}

func TestVersionCommandRuns(t *testing.T) {
	assert.NotPanics(t, func() { versionCmd.Run(versionCmd, nil) })
}

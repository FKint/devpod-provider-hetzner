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

package hetzner

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/FKint/devpod-provider-hetzner/pkg/options"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// userDataVariants are machine configurations that render different parts of the
// cloud-config, so that every conditional in the template is checked.
func userDataVariants(t *testing.T) map[string]*options.Machine {
	t.Helper()

	full := defaultMachine(t)
	full.PersistDocker = true
	full.ContainerdImageStore = true
	full.BuildCacheSize = "5GB"
	full.PruneImagesAfter = "168h"
	full.PruneAnonymousVolumes = true
	full.SwapSizeBytes = 2 << 30
	full.ContainersMemoryHigh = "3G"
	full.ContainersMemoryMax = "3500M"
	full.ContainersSwapMax = "1G"
	full.ContainersTasksMax = "1234"
	full.ContainersCPUQuota = "150%"
	full.Swappiness = "10"

	noPersist := defaultMachine(t)
	noPersist.PersistDocker = false
	noPersist.SwapSizeBytes = 0

	return map[string]*options.Machine{"defaults": defaultMachine(t), "everything on": full, "nothing optional": noPersist}
}

// The boot script runs unattended on a server that is then waited on for minutes, so a
// shell syntax error in it would only show up as a provisioning timeout. These checks
// run on every build; e2e still covers what the script does.
func TestUserDataCommandsAreValidShell(t *testing.T) {
	for name, machine := range userDataVariants(t) {
		t.Run(name, func(t *testing.T) {
			cfg := renderUserData(t, machine)
			require.NotEmpty(t, cfg.RunCmd)

			for _, c := range cfg.RunCmd {
				assert.NotContains(t, c, "<no value>", "an unset template value was rendered")
				assert.NotContains(t, c, "{{", "unrendered template")

				cmd := exec.CommandContext(t.Context(), "sh", "-n")
				cmd.Stdin = strings.NewReader(c)
				out, err := cmd.CombinedOutput()
				assert.NoError(t, err, "not valid shell: %s\n%s", c, out)
			}
		})
	}
}

// shellcheck is preinstalled on the CI runners; elsewhere the test is skipped.
func TestUserDataCommandsPassShellcheck(t *testing.T) {
	shellcheck, err := exec.LookPath("shellcheck")
	if err != nil {
		t.Skip("shellcheck is not installed")
	}

	for name, machine := range userDataVariants(t) {
		t.Run(name, func(t *testing.T) {
			cfg := renderUserData(t, machine)
			script := "#!/bin/sh\n" + strings.Join(cfg.RunCmd, "\n") + "\n"

			cmd := exec.CommandContext(t.Context(), shellcheck, "--shell=sh", "--severity=warning", "-")
			cmd.Stdin = strings.NewReader(script)
			out, err := cmd.CombinedOutput()
			assert.NoError(t, err, "%s\n%s", out, script)
		})
	}
}

func TestUserDataConfigFilesHaveNoUnrenderedTemplate(t *testing.T) {
	for name, machine := range userDataVariants(t) {
		t.Run(name, func(t *testing.T) {
			cfg := renderUserData(t, machine)
			for path, content := range cfg.WriteFiles {
				assert.NotContains(t, content, "<no value>", path)
				assert.NotContains(t, content, "{{", path)
			}
		})
	}
}

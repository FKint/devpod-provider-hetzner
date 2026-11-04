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
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	callDockerPs = "docker ps -q"
	callSync     = "sync"
)

// useFakeDocker puts fake docker and sync commands first in PATH, so the real
// quiesceLocal runner (sh -c ...) runs against them. Each call is appended to the
// returned log. psOutput is what callDockerPs prints; dockerExit its exit status.
func useFakeDocker(t *testing.T, psOutput string, dockerExit int) (logPath string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("needs a POSIX shell")
	}

	dir := t.TempDir()
	logPath = filepath.Join(dir, "calls.log")
	docker := fmt.Sprintf("#!/bin/sh\necho \"docker $*\" >> %q\n[ \"$1\" = ps ] && printf %%b %q\nexit %d\n",
		logPath, psOutput, dockerExit)
	syncCmd := fmt.Sprintf("#!/bin/sh\necho %s >> %q\n", callSync, logPath)
	//nolint:gosec // the fake commands must be executable
	require.NoError(t, os.WriteFile(filepath.Join(dir, "docker"), []byte(docker), 0o700))
	//nolint:gosec // the fake commands must be executable
	require.NoError(t, os.WriteFile(filepath.Join(dir, callSync), []byte(syncCmd), 0o700))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	return logPath
}

func readCalls(t *testing.T, logPath string) []string {
	t.Helper()
	data, err := os.ReadFile(logPath)
	require.NoError(t, err)

	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

func TestQuiesceLocalStopsRunningContainersThenSyncs(t *testing.T) {
	logPath := useFakeDocker(t, "abc\ndef\n", 0)

	(&Hetzner{}).quiesceLocal(t.Context())

	assert.Equal(t, []string{
		callDockerPs,
		fmt.Sprintf("docker stop -t %d abc def", containerStopGrace),
		callSync,
	}, readCalls(t, logPath))
}

func TestQuiesceLocalSkipsDockerStopWithoutContainers(t *testing.T) {
	logPath := useFakeDocker(t, "", 0)

	(&Hetzner{}).quiesceLocal(t.Context())

	assert.Equal(t, []string{callDockerPs, callSync}, readCalls(t, logPath))
}

// A failing docker must not stop the disks from being synced
func TestQuiesceLocalSyncsWhenDockerFails(t *testing.T) {
	logPath := useFakeDocker(t, "abc", 1)

	(&Hetzner{}).quiesceLocal(t.Context())

	assert.Equal(t, []string{
		callDockerPs,
		fmt.Sprintf("docker stop -t %d abc", containerStopGrace),
		callSync,
	}, readCalls(t, logPath))
}

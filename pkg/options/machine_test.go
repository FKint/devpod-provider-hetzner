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
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
)

const infinity = "infinity"

var allMachineEnvs = []string{
	EnvPersistDocker, EnvContainerdImageStore, EnvBuildCacheSize, EnvPruneImagesAfter,
	EnvPruneAnonymousVolumes, EnvSwapSize, EnvContainersMemoryHigh, EnvContainersMemoryMax, EnvContainersSwapMax,
	EnvVolumeFormat,
	EnvContainersTasksMax, EnvContainersCPUQuota, EnvSwappiness,
}

func clearMachineEnv(t *testing.T) {
	t.Helper()
	for _, env := range allMachineEnvs {
		t.Setenv(env, "")
	}
}

func TestMachineFromEnvDefaults(t *testing.T) {
	clearMachineEnv(t)

	m, err := MachineFromEnv()
	assert.NoError(t, err)
	assert.Equal(t, Machine{
		PersistDocker:         true,
		ContainerdImageStore:  true,
		BuildCacheSize:        "5GB",
		PruneImagesAfter:      "168h",
		PruneAnonymousVolumes: true,
		VolumeFormat:          "xfs",
		SwapSizeBytes:         8 << 30,
		ContainersMemoryHigh:  infinity,
		ContainersMemoryMax:   "90%",
		ContainersSwapMax:     "4G",
		ContainersTasksMax:    "32768",
	}, m)
}

func TestMachineFromEnvOverrides(t *testing.T) {
	clearMachineEnv(t)
	t.Setenv(EnvPersistDocker, "false")
	t.Setenv(EnvContainerdImageStore, "false")
	t.Setenv(EnvBuildCacheSize, "500MB")
	t.Setenv(EnvPruneImagesAfter, "Never")
	t.Setenv(EnvPruneAnonymousVolumes, "false")
	t.Setenv(EnvSwapSize, "0")
	t.Setenv(EnvContainersMemoryHigh, "6G")
	t.Setenv(EnvContainersMemoryMax, infinity)
	t.Setenv(EnvContainersSwapMax, "2G")
	t.Setenv(EnvVolumeFormat, "ext4")

	m, err := MachineFromEnv()
	assert.NoError(t, err)
	assert.Equal(t, Machine{
		BuildCacheSize:       "500MB",
		VolumeFormat:         "ext4",
		ContainersMemoryHigh: "6G",
		ContainersMemoryMax:  infinity,
		ContainersSwapMax:    "2G",
		ContainersTasksMax:   "32768",
	}, m)
}

// Zero is refused wherever a limit is a percentage or a size
const (
	zeroPercent = "0%"
	zeroSize    = "0G"
)

func TestMachineFromEnvInvalid(t *testing.T) {
	for env, values := range map[string][]string{
		EnvPersistDocker:        {"yes please"},
		EnvContainerdImageStore: {"maybe"},
		EnvBuildCacheSize:       {"5", "5 GB", "5GiB", "-1GB", "5GB; rm -rf /"},
		EnvPruneImagesAfter: {
			"7d", "0", "0h", "0s", "0.0m", "-1h", "1h || true", "+168h", "1h30m5ms", "0h0m", "99999999999999h", "1\u00b5s",
		},
		EnvPruneAnonymousVolumes: {"2"},
		EnvSwapSize:              {"-1", "8G", "257", "1000"},
		EnvContainersMemoryHigh:  {"85", "101%", "6GB", "6G\nMemoryMax=1", zeroPercent, zeroSize, "85.555%"},
		EnvContainersMemoryMax:   {"lots"},
		// A percentage is not scaled by RAM for swap, so only sizes are accepted
		EnvContainersSwapMax:  {"half", "50%", zeroSize},
		EnvVolumeFormat:       {"btrfs", "XFS", "xfs;"},
		EnvContainersTasksMax: {"0", "-1", "lots", "99999999999", "1 || true"},
		EnvContainersCPUQuota: {zeroPercent, "350", "3.5", "1%\nMemoryMax=1"},
		EnvSwappiness:         {"201", "-1", "60%", "1000", "high"},
	} {
		for _, v := range values {
			clearMachineEnv(t)
			t.Setenv(env, v)
			_, err := MachineFromEnv()
			if assert.Error(t, err, "%s=%q", env, v) {
				assert.Contains(t, err.Error(), env)
			}
		}
	}
}

// The provider.yaml validation patterns must accept the defaults and agree
// with MachineFromEnv on typical values.
func TestMachinePatterns(t *testing.T) {
	for pattern, values := range map[string]map[string]bool{
		PatternDockerSize: {DefaultBuildCacheSize: true, "500mb": true, "5": false, "5 GB": false},
		PatternPruneAge: {
			DefaultPruneImagesAfter: true, "never": true, "1h30m": true, "0h30m": true, "1h0m": true, "0.5s": true,
			"7d": false, "": false, "168": false, "h": false, "-1h": false,
			// Zero and overflow are ParseDuration's to refuse, see TestMachineFromEnvInvalid
			"0h": true,
		},
		PatternDiskSize: {"40": true, " 40 ": true, "007": true, "0": false, "": false, "-5": false, "40GB": false, "4.5": false, "99999": true, "100000": false, "99999999999999999999": false},
		PatternSwapSize: {
			DefaultSwapSize: true, "0": true, "256": true, "007": true, "257": false, "300": false, "1000": false,
			"8G": false, "-1": false, "": false,
		},
		PatternVolumeFormat: {"xfs": true, "ext4": true, "btrfs": false, "xfs ext4": false},
		PatternSystemdMemory: {
			DefaultContainersMemoryHigh: true, DefaultContainersMemoryMax: true, "6G": true, "85.25%": true,
			"85": false, "85.255%": false, "100%": true, "100.00%": true, "0.01%": true, "5%": true, "1G": true,
			zeroPercent: false, "00%": false, "0.00%": false, "101%": false, "100.01%": false, zeroSize: false, "0": false,
		},
		PatternTasksMax:    {DefaultContainersTasksMax: true, infinity: true, "": false, "-1": false},
		PatternCPUQuota:    {"": true, "350%": true, "350": false, "3.5%": false},
		PatternSwappiness:  {"": true, "100": true, "1000": false, "-1": false},
		PatternSystemdSize: {DefaultContainersSwapMax: true, infinity: true, "10G": true, "50%": false, "4": false, zeroSize: false},
	} {
		re := regexp.MustCompile(pattern)
		for v, want := range values {
			assert.Equal(t, want, re.MatchString(v), "%s ~ %q", pattern, v)
		}
	}
}

func TestMachineFromEnvResourceLimits(t *testing.T) {
	clearMachineEnv(t)
	t.Setenv(EnvContainersTasksMax, infinity)
	t.Setenv(EnvContainersCPUQuota, "350%")
	t.Setenv(EnvSwappiness, "150")

	m, err := MachineFromEnv()
	assert.NoError(t, err)
	assert.Equal(t, infinity, m.ContainersTasksMax)
	assert.Equal(t, "350%", m.ContainersCPUQuota)
	assert.Equal(t, "150", m.Swappiness)
}

// The patterns already refuse these; the parsed check is the second line of defence
func TestLimitProblem(t *testing.T) {
	const want = "want a size"
	assert.Empty(t, limitProblem("85%", want))
	assert.Empty(t, limitProblem("100%", want))
	assert.Empty(t, limitProblem("6G", want))
	assert.Contains(t, limitProblem("0%", want), "above 0")
	assert.Contains(t, limitProblem("0G", want), "above 0")
	assert.Contains(t, limitProblem("101%", want), "exceed 100%")
	assert.Equal(t, want, limitProblem("abc", want))
}

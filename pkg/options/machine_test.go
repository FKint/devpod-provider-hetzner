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
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
)

const infinity = "infinity"

var allMachineEnvs = []string{
	EnvPersistDocker, EnvContainerdImageStore, EnvBuildCacheSize, EnvPruneImagesAfter,
	EnvPruneAnonymousVolumes, EnvSwapSize, EnvContainersMemoryHigh, EnvContainersMemoryMax, EnvContainersSwapMax,
	EnvVolumeFormat,
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
	}, m)
}

func TestMachineFromEnvInvalid(t *testing.T) {
	for env, values := range map[string][]string{
		EnvPersistDocker:         {"yes please"},
		EnvContainerdImageStore:  {"maybe"},
		EnvBuildCacheSize:        {"5", "5 GB", "5GiB", "-1GB", "5GB; rm -rf /"},
		EnvPruneImagesAfter:      {"7d", "0", "-1h", "1h || true", "+168h", "1h30m5ms", "1\u00b5s"},
		EnvPruneAnonymousVolumes: {"2"},
		EnvSwapSize:              {"-1", "8G", "1000"},
		EnvContainersMemoryHigh:  {"85", "101%", "6GB", "6G\nMemoryMax=1", "0%", "0G", "85.555%"},
		EnvContainersMemoryMax:   {"lots"},
		// A percentage is not scaled by RAM for swap, so only sizes are accepted
		EnvContainersSwapMax: {"half", "50%", "0G"},
		EnvVolumeFormat:      {"btrfs", "XFS", "xfs;"},
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
		PatternDockerSize:   {DefaultBuildCacheSize: true, "500mb": true, "5": false, "5 GB": false},
		PatternPruneAge:     {DefaultPruneImagesAfter: true, "never": true, "1h30m": true, "7d": false, "": false},
		PatternWholeNumber:  {DefaultSwapSize: true, "0": true, "8G": false, "-1": false},
		PatternVolumeFormat: {"xfs": true, "ext4": true, "btrfs": false, "xfs ext4": false},
		PatternSystemdMemory: {
			DefaultContainersMemoryHigh: true, DefaultContainersMemoryMax: true, "6G": true, "85.25%": true,
			"85": false, "85.255%": false,
		},
		PatternSystemdSize: {DefaultContainersSwapMax: true, infinity: true, "50%": false, "4": false},
	} {
		re := regexp.MustCompile(pattern)
		for v, want := range values {
			assert.Equal(t, want, re.MatchString(v), "%s ~ %q", pattern, v)
		}
	}
}

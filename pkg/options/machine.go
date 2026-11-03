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
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Machine-level options: they configure the server's Docker setup and apply
// each time the server is created, i.e. on the next start after a stop.
// Everything per workspace belongs in devcontainer.json instead.
const (
	EnvPersistDocker         = "PERSIST_DOCKER"
	EnvContainerdImageStore  = "CONTAINERD_IMAGE_STORE"
	EnvBuildCacheSize        = "BUILD_CACHE_SIZE"
	EnvPruneImagesAfter      = "PRUNE_IMAGES_AFTER"
	EnvPruneAnonymousVolumes = "PRUNE_ANONYMOUS_VOLUMES"
	EnvSwapSize              = "SWAP_SIZE"
	EnvContainersMemoryHigh  = "CONTAINERS_MEMORY_HIGH"
	EnvContainersMemoryMax   = "CONTAINERS_MEMORY_MAX"
	EnvContainersSwapMax     = "CONTAINERS_SWAP_MAX"
)

// Defaults for the machine options, shared with the generated provider.yaml.
const (
	DefaultPersistDocker         = "true"
	DefaultContainerdImageStore  = "true"
	DefaultBuildCacheSize        = "5GB"
	DefaultPruneImagesAfter      = "168h"
	DefaultPruneAnonymousVolumes = "true"
	DefaultSwapSize              = "8"
	// No throttle zone by default: measured on a real server, a container
	// allocating past MemoryHigh with full swap was throttled for minutes and
	// never reached MemoryMax to be killed.
	DefaultContainersMemoryHigh = "infinity"
	DefaultContainersMemoryMax  = "90%"
	// Percentage of RAM; keeps part of the swap for the host and makes a runaway
	// container hit MemoryMax (and get OOM-killed) within seconds.
	DefaultContainersSwapMax = "50%"
)

// PruneNever disables the image prune at start.
const PruneNever = "never"

// maxSwapSizeGB keeps a typo from filling the root disk.
const maxSwapSizeGB = 256

// Validation patterns, shared with the generated provider.yaml so DevPod
// rejects bad values before the provider runs.
const (
	PatternDockerSize = `^(?i)\d+(\.\d+)?(B|KB|MB|GB|TB)$`
	// A percentage of RAM, a size with a K/M/G/T suffix, or infinity. Bare
	// byte counts are refused: "85" is almost surely meant as 85%.
	PatternSystemdMemory = `^(\d+(\.\d+)?%|\d+[KMGT]|infinity)$`
	PatternPruneAge      = `^((?i)never|(\d+(\.\d+)?(h|m|s))+)$`
	PatternWholeNumber   = `^\d+$`
)

var (
	dockerSizeRe    = regexp.MustCompile(PatternDockerSize)
	systemdMemoryRe = regexp.MustCompile(PatternSystemdMemory)
)

// Machine is the validated machine-level configuration used to render the
// server's cloud-config. Every field is safe to embed in YAML, JSON and
// systemd units.
type Machine struct {
	// PersistDocker keeps Docker's and containerd's data (images, containers,
	// named volumes, build cache) on the volume. Otherwise it is on the
	// server's local disk and lost on every stop.
	PersistDocker bool
	// ContainerdImageStore enables Docker's containerd image store.
	ContainerdImageStore bool
	// BuildCacheSize is the BuildKit cache size kept by garbage collection, e.g. "5GB".
	BuildCacheSize string
	// PruneImagesAfter is the age (Go duration) after which images not used by
	// any container are removed at start. Empty disables the prune.
	PruneImagesAfter string
	// PruneAnonymousVolumes removes unused anonymous volumes at start.
	PruneAnonymousVolumes bool
	// SwapSizeBytes is the swap file size on the local disk; 0 disables swap.
	SwapSizeBytes int64
	// ContainersMemoryHigh, ContainersMemoryMax and ContainersSwapMax cap all
	// containers together (systemd MemoryHigh/MemoryMax/MemorySwapMax values).
	ContainersMemoryHigh string
	ContainersMemoryMax  string
	ContainersSwapMax    string
}

// MachineFromEnv reads and validates the machine options. Unset options get
// their defaults, so an older provider.yaml without them keeps working.
func MachineFromEnv() (Machine, error) {
	var (
		m    Machine
		errs []string
		err  error
	)
	get := func(name, def string) string {
		if v := strings.TrimSpace(os.Getenv(name)); v != "" {
			return v
		}
		return def
	}
	fail := func(name, value, want string) {
		errs = append(errs, fmt.Sprintf("%s=%q: %s", name, value, want))
	}

	parseBool := func(name, def string) bool {
		v := get(name, def)
		b, err := strconv.ParseBool(v)
		if err != nil {
			fail(name, v, "want true or false")
		}
		return b
	}
	m.PersistDocker = parseBool(EnvPersistDocker, DefaultPersistDocker)
	m.ContainerdImageStore = parseBool(EnvContainerdImageStore, DefaultContainerdImageStore)
	m.PruneAnonymousVolumes = parseBool(EnvPruneAnonymousVolumes, DefaultPruneAnonymousVolumes)

	m.BuildCacheSize = get(EnvBuildCacheSize, DefaultBuildCacheSize)
	if !dockerSizeRe.MatchString(m.BuildCacheSize) {
		fail(EnvBuildCacheSize, m.BuildCacheSize, "want a size such as 5GB or 500MB")
	}

	m.PruneImagesAfter = get(EnvPruneImagesAfter, DefaultPruneImagesAfter)
	if strings.EqualFold(m.PruneImagesAfter, PruneNever) {
		m.PruneImagesAfter = ""
	} else if d, perr := time.ParseDuration(m.PruneImagesAfter); perr != nil || d <= 0 {
		fail(EnvPruneImagesAfter, m.PruneImagesAfter, "want a duration such as 168h, or never")
	}

	swap := get(EnvSwapSize, DefaultSwapSize)
	var swapGB int64
	if swapGB, err = strconv.ParseInt(swap, 10, 64); err != nil || swapGB < 0 || swapGB > maxSwapSizeGB {
		fail(EnvSwapSize, swap, fmt.Sprintf("want whole GB between 0 (no swap) and %d", maxSwapSizeGB))
	} else {
		m.SwapSizeBytes = swapGB << 30
	}

	parseMemory := func(name, def string) string {
		v := get(name, def)
		if !systemdMemoryRe.MatchString(v) {
			fail(name, v, "want a percentage of RAM (85%), a size (6G) or infinity")
		} else if pct, ok := strings.CutSuffix(v, "%"); ok {
			if p, _ := strconv.ParseFloat(pct, 64); p > 100 {
				fail(name, v, "a percentage can't exceed 100%")
			}
		}
		return v
	}
	m.ContainersMemoryHigh = parseMemory(EnvContainersMemoryHigh, DefaultContainersMemoryHigh)
	m.ContainersMemoryMax = parseMemory(EnvContainersMemoryMax, DefaultContainersMemoryMax)
	m.ContainersSwapMax = parseMemory(EnvContainersSwapMax, DefaultContainersSwapMax)

	if len(errs) > 0 {
		return Machine{}, fmt.Errorf("invalid machine options: %s", strings.Join(errs, "; "))
	}

	return m, nil
}

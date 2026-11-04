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
	EnvContainersTasksMax    = "CONTAINERS_TASKS_MAX"
	EnvContainersCPUQuota    = "CONTAINERS_CPU_QUOTA"
	EnvSwappiness            = "SWAPPINESS"
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
	// Keeps part of the swap for the host and makes a runaway container hit
	// MemoryMax (and get OOM-killed) within seconds. A size, not a percentage:
	// systemd documents percentages of RAM for MemoryMax, not for MemorySwapMax.
	DefaultContainersSwapMax = "4G"
	// Generous for parallel builds, but stops a fork bomb before the PID table fills.
	DefaultContainersTasksMax = "32768"
	// DefaultContainersCPUQuota and DefaultSwappiness are empty: no CPU cap, kernel default.
	DefaultContainersCPUQuota = ""
	DefaultSwappiness         = ""
)

// PruneNever disables the image prune at start.
const PruneNever = "never"

// maxSwapSizeGB keeps a typo from filling the root disk.
const maxSwapSizeGB = 256

// Validation patterns, shared with the generated provider.yaml so DevPod
// rejects bad values before the provider runs.
const (
	PatternDockerSize = `^(?i)\d+(\.\d+)?(B|KB|MB|GB|TB)$`
	// A percentage of RAM (at most two decimals, what systemd parses), a size with
	// a K/M/G/T suffix, or infinity. Bare byte counts are refused: "85" is almost
	// surely meant as 85%.
	PatternSystemdMemory = `^(\d+(\.\d{1,2})?%|\d+[KMGT]|infinity)$`
	// A size or infinity, for limits that systemd does not scale by RAM
	PatternSystemdSize = `^(\d+[KMGT]|infinity)$`
	PatternPruneAge    = `^((?i)never|(\d+(\.\d+)?[hms])+)$`
	PatternWholeNumber = `^\d+$`
	// A process limit, or infinity
	PatternTasksMax = `^(\d+|infinity)$`
	// Empty (no cap) or a percentage of one CPU: 350% is 3.5 CPUs
	PatternCPUQuota = `^(\d+%)?$`
	// Empty (kernel default, 60) or 0-200
	PatternSwappiness = `^(\d{1,3})?$`
)

var (
	dockerSizeRe    = regexp.MustCompile(PatternDockerSize)
	systemdMemoryRe = regexp.MustCompile(PatternSystemdMemory)
	systemdSizeRe   = regexp.MustCompile(PatternSystemdSize)
	pruneAgeRe      = regexp.MustCompile(PatternPruneAge)
	tasksMaxRe      = regexp.MustCompile(PatternTasksMax)
	cpuQuotaRe      = regexp.MustCompile(PatternCPUQuota)
	swappinessRe    = regexp.MustCompile(PatternSwappiness)
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
	// ContainersTasksMax caps the processes and threads of all containers together.
	ContainersTasksMax string
	// ContainersCPUQuota caps the CPU time of all containers together, as a
	// percentage of one CPU. Empty means no cap.
	ContainersCPUQuota string
	// Swappiness is vm.swappiness. Empty leaves the kernel default.
	Swappiness string
}

// envParser reads options from the environment and collects every problem.
type envParser struct{ errs []string }

func (p *envParser) fail(name, value, want string) {
	p.errs = append(p.errs, fmt.Sprintf("%s=%q: %s", name, value, want))
}

func (p *envParser) get(name, def string) string {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}
	return def
}

func (p *envParser) boolean(name, def string) bool {
	v := p.get(name, def)
	b, err := strconv.ParseBool(v)
	if err != nil {
		p.fail(name, v, "want true or false")
	}
	return b
}

// pattern returns the value if it matches re and records an error otherwise.
func (p *envParser) pattern(name, def string, re *regexp.Regexp, want string) (string, bool) {
	v := p.get(name, def)
	if !re.MatchString(v) {
		p.fail(name, v, want)
		return v, false
	}
	return v, true
}

// limit reads a systemd memory limit.
func (p *envParser) limit(name, def string, re *regexp.Regexp, want string) string {
	v, ok := p.pattern(name, def, re, want)
	if !ok || v == "infinity" {
		return v
	}
	// The patterns guarantee a number. 0 would make every container fail on its first allocation.
	n, _ := strconv.ParseFloat(strings.TrimRight(v, "%KMGT"), 64)
	switch {
	case n == 0:
		p.fail(name, v, "must be above 0 (use infinity for no limit)")
	case strings.HasSuffix(v, "%") && n > 100:
		p.fail(name, v, "a percentage can't exceed 100%")
	}

	return v
}

// tasksMax reads CONTAINERS_TASKS_MAX.
func (p *envParser) tasksMax() string {
	v, ok := p.pattern(EnvContainersTasksMax, DefaultContainersTasksMax, tasksMaxRe, "want a number or infinity")
	if ok && v != "infinity" {
		if n, err := strconv.ParseUint(v, 10, 32); err != nil || n == 0 {
			p.fail(EnvContainersTasksMax, v, "must be between 1 and 4294967295 (use infinity for no limit)")
		}
	}

	return v
}

// cpuQuota reads CONTAINERS_CPU_QUOTA; empty means no cap.
func (p *envParser) cpuQuota() string {
	v, ok := p.pattern(EnvContainersCPUQuota, DefaultContainersCPUQuota, cpuQuotaRe, "want a percentage of one CPU such as 350%")
	if ok && v != "" {
		if n, err := strconv.ParseUint(strings.TrimSuffix(v, "%"), 10, 32); err != nil || n == 0 {
			p.fail(EnvContainersCPUQuota, v, "must be above 0%")
		}
	}

	return v
}

// swappiness reads SWAPPINESS; empty means the kernel default.
func (p *envParser) swappiness() string {
	v, ok := p.pattern(EnvSwappiness, DefaultSwappiness, swappinessRe, "want a number between 0 and 200")
	if ok && v != "" {
		if n, _ := strconv.Atoi(v); n > 200 {
			p.fail(EnvSwappiness, v, "want a number between 0 and 200")
		}
	}

	return v
}

// pruneAge reads PRUNE_IMAGES_AFTER; "never" becomes "".
func (p *envParser) pruneAge() string {
	v, ok := p.pattern(EnvPruneImagesAfter, DefaultPruneImagesAfter, pruneAgeRe, "want a duration such as 168h, or never")
	switch {
	case !ok:
		return v
	case strings.EqualFold(v, PruneNever):
		return ""
	}
	if d, err := time.ParseDuration(v); err != nil || d <= 0 {
		p.fail(EnvPruneImagesAfter, v, "want a duration such as 168h, or never")
	}

	return v
}

func (p *envParser) swapBytes() int64 {
	v := p.get(EnvSwapSize, DefaultSwapSize)
	gb, err := strconv.ParseInt(v, 10, 64)
	if err != nil || gb < 0 || gb > maxSwapSizeGB {
		p.fail(EnvSwapSize, v, fmt.Sprintf("want whole GB between 0 (no swap) and %d", maxSwapSizeGB))
		return 0
	}

	return gb << 30
}

// MachineFromEnv reads and validates the machine options. Unset options get
// their defaults, so an older provider.yaml without them keeps working.
func MachineFromEnv() (Machine, error) {
	const ramLimit = "want a percentage of RAM (85%), a size (6G) or infinity"

	var p envParser
	m := Machine{
		PersistDocker:         p.boolean(EnvPersistDocker, DefaultPersistDocker),
		ContainerdImageStore:  p.boolean(EnvContainerdImageStore, DefaultContainerdImageStore),
		PruneAnonymousVolumes: p.boolean(EnvPruneAnonymousVolumes, DefaultPruneAnonymousVolumes),
		PruneImagesAfter:      p.pruneAge(),
		SwapSizeBytes:         p.swapBytes(),
		ContainersMemoryHigh:  p.limit(EnvContainersMemoryHigh, DefaultContainersMemoryHigh, systemdMemoryRe, ramLimit),
		ContainersMemoryMax:   p.limit(EnvContainersMemoryMax, DefaultContainersMemoryMax, systemdMemoryRe, ramLimit),
		ContainersSwapMax:     p.limit(EnvContainersSwapMax, DefaultContainersSwapMax, systemdSizeRe, "want a size (4G) or infinity"),
		ContainersTasksMax:    p.tasksMax(),
		ContainersCPUQuota:    p.cpuQuota(),
		Swappiness:            p.swappiness(),
	}
	m.BuildCacheSize, _ = p.pattern(EnvBuildCacheSize, DefaultBuildCacheSize, dockerSizeRe, "want a size such as 5GB or 500MB")

	if len(p.errs) > 0 {
		return Machine{}, fmt.Errorf("invalid machine options: %s", strings.Join(p.errs, "; "))
	}

	return m, nil
}

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

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/loft-sh/devpod/pkg/provider"
	"github.com/loft-sh/devpod/pkg/types"
	"sigs.k8s.io/yaml"
)

const (
	archAMD64 = "amd64"
	archARM64 = "arm64"

	// defaultMachineType must be offered by Hetzner when the provider.yaml is
	// generated; Hetzner retires server types over time (cx32 became cx33).
	defaultMachineType = "cx33"
)

type platform struct{ os, arch string }

// binaryName is the file name of the provider binary for this platform, as
// produced by hack/build.sh.
func (p platform) binaryName() string {
	name := fmt.Sprintf("devpod-provider-hetzner-%s-%s", p.os, p.arch)
	if p.os == "windows" {
		name += ".exe"
	}

	return name
}

// platforms lists every binary published with a release. Only the linux ones
// are also injected into the remote agent.
var platforms = []platform{
	{"linux", archAMD64},
	{"linux", archARM64},
	{"darwin", archAMD64},
	{"darwin", archARM64},
	{"windows", archAMD64},
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "Expected version as argument")
		os.Exit(1)
		return
	}

	token := os.Getenv("HCLOUD_TOKEN")
	if token == "" {
		fmt.Fprintln(os.Stderr, "Expected HCLOUD_TOKEN environment variable")
		os.Exit(1)
	}

	ctx := context.TODO()
	h := hcloud.NewClient(hcloud.WithToken(token))

	locations, err := h.Location.All(ctx)
	if err != nil {
		panic(err)
	}

	var regions types.OptionEnumArray
	for _, l := range locations {
		regions = append(regions, types.OptionEnum{
			Value:       l.Name,
			DisplayName: l.City,
		})
	}

	serverTypes, err := h.ServerType.All(ctx)
	if err != nil {
		panic(err)
	}

	var machineTypes types.OptionEnumArray
	for _, t := range serverTypes {
		if t.IsDeprecated() {
			continue
		}

		machineTypes = append(machineTypes, types.OptionEnum{
			Value:       t.Name,
			DisplayName: fmt.Sprintf("%d vCPUs, %.0F GB RAM (%s)", t.Cores, t.Memory, t.Name),
		})
	}

	// A default Hetzner no longer offers would make every default "devpod up" fail,
	// so refuse to generate a provider.yaml with one
	if err := checkMachineTypeOffered(defaultMachineType, machineTypes); err != nil {
		panic(err)
	}

	checksums := map[string]string{}
	for _, p := range platforms {
		checksum, err := File(filepath.Join("dist", p.binaryName()))
		if err != nil {
			panic(fmt.Errorf("generate checksum for %s: %w", p.binaryName(), err))
		}

		checksums[p.binaryName()] = checksum
	}

	version := fmt.Sprintf("v%s", os.Args[1])

	s, err := yaml.Marshal(BuildConfig(
		version,
		checksums,
		"nbg1",
		regions,
		defaultMachineType,
		machineTypes,
	))
	if err != nil {
		panic(err)
	}

	fmt.Print(string(s))
}

//nolint:funlen // ignore
func BuildConfig(
	version string,
	checksums map[string]string,
	defaultRegion string,
	regions types.OptionEnumArray,
	defaultType string,
	machineTypes types.OptionEnumArray,
) provider.ProviderConfig {
	releaseURLBase := fmt.Sprintf("https://github.com/FKint/devpod-provider-hetzner/releases/download/%s", version)
	binaries := func(onlyOS string) []*provider.ProviderBinary {
		var out []*provider.ProviderBinary
		for _, p := range platforms {
			if onlyOS != "" && p.os != onlyOS {
				continue
			}

			out = append(out, &provider.ProviderBinary{
				OS:       p.os,
				Arch:     p.arch,
				Path:     releaseURLBase + "/" + p.binaryName(),
				Checksum: checksums[p.binaryName()],
			})
		}

		return out
	}

	return provider.ProviderConfig{
		Name:        "hetzner",
		Version:     version,
		Description: "DevPod on Hetzner",
		Icon:        "https://raw.githubusercontent.com/FKint/devpod-provider-hetzner/main/assets/hetzner.png",
		OptionGroups: []provider.ProviderOptionGroup{
			{
				Name:           "Hetzner options",
				DefaultVisible: true,
				Options: []string{
					"DISK_SIZE",
					"DISK_IMAGE",
					"MACHINE_TYPE",
				},
			},
			{
				Name:           "Agent options",
				DefaultVisible: false,
				Options: []string{
					"AGENT_PATH",
					"AGENT_DATA_PATH",
					"INACTIVITY_TIMEOUT",
					"INJECT_DOCKER_CREDENTIALS",
					"INJECT_GIT_CREDENTIALS",
				},
			},
		},
		Options: map[string]*types.Option{
			"TOKEN": {
				Description: "The Hetzner API token to use.",
				Required:    true,
				Password:    true,
				Command: `if [ ! -z "${HETZNER_TOKEN}" ]; then
	echo ${HETZNER_TOKEN}
elif [ ! -z "${HETZNER_ACCESS_TOKEN}" ]; then
	echo ${HETZNER_ACCESS_TOKEN}
fi`,
			},
			"REGION": {
				Description: "The Hetzner region to use. E.g. nbg1",
				Required:    true,
				Default:     defaultRegion,
				Enum:        regions,
				Local:       true,
			},
			"DISK_SIZE": {
				Description: "The disk size in GB.",
				Default:     "30",
				Local:       true,
			},
			"DISK_IMAGE": {
				Description: "The disk image to use.",
				Default:     "docker-ce",
				Local:       true,
			},
			"MACHINE_TYPE": {
				Description: "The machine type to use.",
				Default:     defaultType,
				Enum:        machineTypes,
				Local:       true,
			},
			"INACTIVITY_TIMEOUT": {
				Description: "If defined, will automatically stop the VM after the inactivity period.",
				Default:     "10m",
			},
			"INJECT_GIT_CREDENTIALS": {
				Description: "If DevPod should inject git credentials into the remote host.",
				Default:     "true",
			},
			"INJECT_DOCKER_CREDENTIALS": {
				Description: "If DevPod should inject docker credentials into the remote host.",
				Default:     "true",
			},
			"AGENT_PATH": {
				Description: "The path where to inject the DevPod agent to.",
				Default:     "/home/devpod/.devpod/devpod",
			},
			"AGENT_DATA_PATH": {
				Description: "The path where to store the agent data.",
				Default:     "/home/devpod/.devpod/agent",
			},
		},
		Agent: provider.ProviderAgentConfig{ //nolint:gosec // field names, not credentials
			Path:                    "${AGENT_PATH}",
			DataPath:                "${AGENT_DATA_PATH}",
			Timeout:                 "${INACTIVITY_TIMEOUT}",
			InjectGitCredentials:    "${INJECT_GIT_CREDENTIALS}",
			InjectDockerCredentials: "${INJECT_DOCKER_CREDENTIALS}",
			Binaries:                map[string][]*provider.ProviderBinary{"HETZNER_PROVIDER": binaries("linux")},
			Exec: provider.ProviderAgentConfigExec{
				Shutdown: types.StrArray{"${HETZNER_PROVIDER} stop"},
			},
		},
		Binaries: map[string][]*provider.ProviderBinary{"HETZNER_PROVIDER": binaries("")},
		Exec: provider.ProviderCommands{
			Init:    types.StrArray{"${HETZNER_PROVIDER} init"},
			Command: types.StrArray{"${HETZNER_PROVIDER} command"},
			Create:  types.StrArray{"${HETZNER_PROVIDER} create"},
			Delete:  types.StrArray{"${HETZNER_PROVIDER} delete"},
			Start:   types.StrArray{"${HETZNER_PROVIDER} start"},
			Stop:    types.StrArray{"${HETZNER_PROVIDER} stop"},
			Status:  types.StrArray{"${HETZNER_PROVIDER} status"},
		},
	}
}

// checkMachineTypeOffered fails if the default machine type isn't one of the offered ones.
func checkMachineTypeOffered(def string, offered types.OptionEnumArray) error {
	for _, o := range offered {
		if o.Value == def {
			return nil
		}
	}

	return fmt.Errorf("default MACHINE_TYPE %q is not offered by Hetzner, or is deprecated (%d offered); update defaultMachineType", def, len(offered))
}

// File hashes a given file to a sha256 string
func File(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer func() {
		err = file.Close()
	}()

	hash := sha256.New()
	_, err = io.Copy(hash, file)
	if err != nil {
		return "", err
	}

	return strings.ToLower(hex.EncodeToString(hash.Sum(nil))), err
}

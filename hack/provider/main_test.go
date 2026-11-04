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

package main

import (
	"bytes"
	"regexp"
	"testing"

	"github.com/FKint/devpod-provider-hetzner/pkg/options"
	"github.com/loft-sh/devpod/pkg/provider"
	"github.com/loft-sh/devpod/pkg/types"
	"github.com/stretchr/testify/assert"
	"sigs.k8s.io/yaml"
)

// The generated provider.yaml must pass DevPod's own validation.
func TestBuildConfigIsValidProvider(t *testing.T) {
	checksums := map[string]string{}
	for _, p := range platforms {
		checksums[p.binaryName()] = "0000"
	}
	cfg := BuildConfig("v0.0.0", checksums, "nbg1",
		types.OptionEnumArray{{Value: "nbg1"}}, "cx33", types.OptionEnumArray{{Value: "cx33"}})

	out, err := yaml.Marshal(cfg)
	if !assert.NoError(t, err) {
		return
	}
	parsed, err := provider.ParseProvider(bytes.NewReader(out))
	if !assert.NoError(t, err) {
		return
	}

	// Every machine option is exposed, grouped, and its default passes its own validation
	machineOptions := []string{
		options.EnvPersistDocker, options.EnvContainerdImageStore, options.EnvBuildCacheSize,
		options.EnvPruneImagesAfter, options.EnvPruneAnonymousVolumes, options.EnvSwapSize,
		options.EnvContainersMemoryHigh, options.EnvContainersMemoryMax, options.EnvContainersSwapMax,
		options.EnvVolumeFormat,
	}
	var grouped []string
	for _, g := range parsed.OptionGroups {
		grouped = append(grouped, g.Options...)
	}
	for _, name := range machineOptions {
		opt, ok := parsed.Options[name]
		if !assert.True(t, ok, name) {
			continue
		}
		assert.Contains(t, grouped, name)
		assert.True(t, opt.Local, name)
		assert.NotEmpty(t, opt.Default, name)
		if opt.ValidationPattern != "" {
			assert.Regexp(t, regexp.MustCompile(opt.ValidationPattern), opt.Default, name)
		}
	}
}

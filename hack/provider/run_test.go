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
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const binary = "provider"

func TestFileSHA256(t *testing.T) {
	file := filepath.Join(t.TempDir(), "binary")
	require.NoError(t, os.WriteFile(file, []byte("abc"), 0o600))

	got, err := fileSHA256(file)
	require.NoError(t, err)
	// The well-known SHA-256 of "abc"
	assert.Equal(t, "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad", got)

	_, err = fileSHA256(filepath.Join(t.TempDir(), "missing"))
	assert.Error(t, err)
}

// These fail before any API call, so a release without a token fails fast and clearly
func TestRunValidatesItsInputBeforeCallingTheAPI(t *testing.T) {
	t.Setenv("HCLOUD_TOKEN", "")

	for _, args := range [][]string{{binary}, {binary, "1.0.0", "extra"}} {
		out := new(bytes.Buffer)
		err := run(context.Background(), args, out)
		assert.ErrorContains(t, err, "usage", args)
		assert.Empty(t, out.String())
	}

	err := run(context.Background(), []string{binary, "1.0.0"}, new(bytes.Buffer))
	assert.ErrorContains(t, err, "HCLOUD_TOKEN")
	err = run(context.Background(), []string{binary, checkOnlyArg}, new(bytes.Buffer))
	assert.ErrorContains(t, err, "HCLOUD_TOKEN")
}

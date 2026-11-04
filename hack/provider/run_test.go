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
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/FKint/devpod-provider-hetzner/internal/fakehttp"
	"github.com/loft-sh/devpod/pkg/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	binary         = "provider"
	testVersion    = "1.0.0"
	releaseVersion = "1.2.3"
)

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

	for _, args := range [][]string{{binary}, {binary, testVersion, "extra"}} {
		out := new(bytes.Buffer)
		err := run(context.Background(), args, out)
		assert.ErrorContains(t, err, "usage", args)
		assert.Empty(t, out.String())
	}

	err := run(context.Background(), []string{binary, testVersion}, new(bytes.Buffer))
	assert.ErrorContains(t, err, "HCLOUD_TOKEN")
	err = run(context.Background(), []string{binary, checkOnlyArg}, new(bytes.Buffer))
	assert.ErrorContains(t, err, "HCLOUD_TOKEN")
}

// useFakeAPI answers the Hetzner API with canned lists. run builds its own client on
// http.DefaultTransport, so replacing that keeps every request in-process. serverTypes
// is the JSON array of server types; an empty string makes every request fail with a 401.
func useFakeAPI(t *testing.T, serverTypes string) {
	t.Helper()

	fakehttp.ReplaceDefaultTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case serverTypes == "":
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"error":{"code":"unauthorized","message":"denied"}}`) //nolint:misspell // the API's code
		case strings.HasSuffix(r.URL.Path, "/locations"):
			_, _ = io.WriteString(w, `{"locations":[{"id":1,"name":"nbg1","city":"Nuremberg"},{"id":2,"name":"fsn1","city":"Falkenstein"}]}`)
		case strings.HasSuffix(r.URL.Path, "/server_types"):
			_, _ = io.WriteString(w, `{"server_types":`+serverTypes+`}`)
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"error":{"code":"not_found","message":"unexpected request"}}`)
		}
	}))
}

const (
	offeredTypes = `[{"id":1,"name":"cx33","cores":4,"memory":8},` +
		`{"id":2,"name":"cx22","cores":2,"memory":4,` +
		`"deprecation":{"announced":"2026-01-01T00:00:00Z","unavailable_after":"2026-06-01T00:00:00Z"}}]`
	retiredDefault = `[{"id":2,"name":"cx22","cores":2,"memory":4}]`
)

// writeDist creates dist/ with a binary for every platform, in a directory that becomes
// the working directory (run reads the binaries from ./dist).
func writeDist(t *testing.T, missing ...string) {
	t.Helper()

	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, "dist"), 0o700))
	for _, p := range platforms {
		if !slices.Contains(missing, p.binaryName()) {
			require.NoError(t, os.WriteFile(filepath.Join(dir, "dist", p.binaryName()), []byte(p.binaryName()), 0o600))
		}
	}
	t.Chdir(dir)
}

func TestRunCheckOnlyVerifiesTheDefaultMachineType(t *testing.T) {
	t.Setenv("HCLOUD_TOKEN", "token")

	useFakeAPI(t, offeredTypes)
	out := new(bytes.Buffer)
	assert.NoError(t, run(context.Background(), []string{binary, checkOnlyArg}, out))
	assert.Empty(t, out.String(), "check generates nothing")

	useFakeAPI(t, retiredDefault)
	assert.ErrorContains(t, run(context.Background(), []string{binary, checkOnlyArg}, new(bytes.Buffer)), defaultMachineType)
}

func TestRunReportsAPIErrors(t *testing.T) {
	t.Setenv("HCLOUD_TOKEN", "token")
	useFakeAPI(t, "")

	assert.ErrorContains(t, run(context.Background(), []string{binary, testVersion}, new(bytes.Buffer)), "denied")
}

func TestRunGeneratesAValidProviderYAML(t *testing.T) {
	t.Setenv("HCLOUD_TOKEN", "token")
	useFakeAPI(t, offeredTypes)
	writeDist(t)

	out := new(bytes.Buffer)
	require.NoError(t, run(context.Background(), []string{binary, releaseVersion}, out))

	parsed, err := provider.ParseProvider(bytes.NewReader(out.Bytes()))
	require.NoError(t, err)
	assert.Equal(t, "v"+releaseVersion, parsed.Version)
	assert.Equal(t, "nbg1", parsed.Options["REGION"].Default)
	assert.Contains(t, out.String(), "Falkenstein", "every location is offered")
	assert.Contains(t, out.String(), "4 vCPUs, 8 GB RAM (cx33)")
	assert.NotContains(t, out.String(), "cx22", "a deprecated server type is not offered")
	assert.Contains(t, out.String(), "v"+releaseVersion+"/devpod-provider-hetzner-linux-amd64")
}

func TestRunNeedsEveryBinary(t *testing.T) {
	t.Setenv("HCLOUD_TOKEN", "token")
	useFakeAPI(t, offeredTypes)
	writeDist(t, platforms[2].binaryName())

	out := new(bytes.Buffer)
	err := run(context.Background(), []string{binary, releaseVersion}, out)
	assert.ErrorContains(t, err, platforms[2].binaryName())
	assert.Empty(t, out.String(), "no half-generated provider.yaml")
}

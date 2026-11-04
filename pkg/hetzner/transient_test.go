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
	"context"
	"errors"
	"fmt"
	"net"
	"testing"

	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/stretchr/testify/assert"
)

func TestIsTransient(t *testing.T) {
	ctx := context.Background()
	apiErr := func(code hcloud.ErrorCode) error { return hcloud.Error{Code: code} }

	assert.True(t, isTransient(ctx, &net.DNSError{IsTimeout: true}), "network errors are retried")
	assert.True(t, isTransient(ctx, fmt.Errorf("wrapped: %w", &net.DNSError{})))
	assert.True(t, isTransient(ctx, apiErr(hcloud.ErrorCodeRateLimitExceeded)))
	assert.True(t, isTransient(ctx, apiErr(hcloud.ErrorCodeTimeout)))
	assert.True(t, isTransient(ctx, apiErr(hcloud.ErrorCodeServerError)))
	assert.True(t, isTransient(ctx, apiErr(hcloud.ErrorCodeConflict)), "a locked resource is released by the action holding it")
	assert.False(t, isTransient(ctx, apiErr(hcloud.ErrorCodeUnauthorized)))
	assert.False(t, isTransient(ctx, apiErr(hcloud.ErrorCodeUniquenessError)), "a duplicate name stays a duplicate")
	assert.False(t, isTransient(ctx, errors.New("boom")))
	assert.False(t, isTransient(ctx, nil))

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	assert.False(t, isTransient(cancelled, &net.DNSError{IsTimeout: true}), "a caller that gave up is not retried")
}

func TestParseCloudInitStatusRejectsGarbage(t *testing.T) {
	_, err := parseCloudInitStatus([]byte("status: [unterminated"))
	assert.Error(t, err)
}

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
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestPollUntilChecksImmediately(t *testing.T) {
	var calls int
	// The interval is far longer than the test: a done first check must not wait for it
	err := pollUntil(context.Background(), time.Minute, time.Hour, func(context.Context) (bool, error) {
		calls++
		return true, nil
	})
	assert.NoError(t, err)
	assert.Equal(t, 1, calls)
}

func TestPollUntilRepeatsUntilDone(t *testing.T) {
	var calls int
	err := pollUntil(context.Background(), time.Minute, time.Millisecond, func(context.Context) (bool, error) {
		calls++
		return calls == 4, nil
	})
	assert.NoError(t, err)
	assert.Equal(t, 4, calls)
}

func TestPollUntilStopsOnCheckError(t *testing.T) {
	boom := errors.New("boom")
	var calls int
	err := pollUntil(context.Background(), time.Minute, time.Millisecond, func(context.Context) (bool, error) {
		calls++
		return false, boom
	})
	assert.Same(t, boom, err, "the check's error is returned as is")
	assert.Equal(t, 1, calls)
	assert.False(t, isContextErr(err))
}

func TestPollUntilTimeout(t *testing.T) {
	var sawDeadline atomic.Bool
	err := pollUntil(context.Background(), 20*time.Millisecond, time.Millisecond, func(ctx context.Context) (bool, error) {
		_, ok := ctx.Deadline()
		sawDeadline.Store(ok)
		return false, nil
	})
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.True(t, isContextErr(err))
	assert.True(t, sawDeadline.Load(), "the context passed to check carries the timeout")
}

func TestPollUntilCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var calls int
	err := pollUntil(ctx, time.Minute, time.Millisecond, func(context.Context) (bool, error) {
		calls++
		if calls == 2 {
			cancel()
		}
		return false, nil
	})
	assert.ErrorIs(t, err, context.Canceled)
	assert.True(t, isContextErr(err))
	assert.Equal(t, 2, calls)
}

func TestPollUntilTimeoutInterruptsTheWait(t *testing.T) {
	start := time.Now()
	err := pollUntil(context.Background(), 20*time.Millisecond, time.Hour, func(context.Context) (bool, error) {
		return false, nil
	})
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, time.Since(start), 5*time.Second, "must not sleep out the whole interval")
}

func TestIsContextErr(t *testing.T) {
	assert.True(t, isContextErr(context.Canceled))
	assert.True(t, isContextErr(context.DeadlineExceeded))
	assert.True(t, isContextErr(errors.Join(errors.New("wrapped"), context.DeadlineExceeded)))
	assert.False(t, isContextErr(errors.New(otherName)))
	assert.False(t, isContextErr(nil))
}

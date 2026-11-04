/*
 * Copyright 2023 Simon Emms <simon@simonemms.com>
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

package cmd

import (
	"context"

	"github.com/FKint/devpod-provider-hetzner/pkg/hetzner"
	"github.com/FKint/devpod-provider-hetzner/pkg/options"
	"github.com/loft-sh/devpod/pkg/client"
)

// runAndWait runs an action against the machine, then waits for it to reach the wanted status.
func runAndWait(action func(*hetzner.Hetzner, context.Context, string) error, want client.Status) error {
	opts, err := options.FromEnv(false)
	if err != nil {
		return err
	}

	ctx := context.Background()
	h := hetzner.NewHetzner(opts.Token)

	if err := action(h, ctx, opts.MachineID); err != nil {
		return err
	}

	return h.WaitForStatus(ctx, opts.MachineID, want)
}

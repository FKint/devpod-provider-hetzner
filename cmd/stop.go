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

package cmd

import (
	"context"

	"github.com/FKint/devpod-provider-hetzner/pkg/hetzner"
	"github.com/loft-sh/devpod/pkg/client"
	"github.com/spf13/cobra"
)

// stopCmd represents the stop command
var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop an instance",
	RunE: func(_ *cobra.Command, _ []string) error {
		return runAndWait(func(h *hetzner.Hetzner, ctx context.Context, name string) error {
			return h.Stop(ctx, name, fromMachine)
		}, client.StatusStopped)
	},
}

// fromMachine is set when the command runs on the machine it stops (the inactivity
// shutdown), where the machine can't be powered off before the server is deleted.
var fromMachine bool

func init() {
	stopCmd.Flags().BoolVar(&fromMachine, "from-machine", false,
		"the command runs on the machine being stopped: stop the containers and sync instead of an ACPI shutdown")
	rootCmd.AddCommand(stopCmd)
}

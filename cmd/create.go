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
	"errors"

	"github.com/FKint/devpod-provider-hetzner/pkg/hetzner"
	"github.com/FKint/devpod-provider-hetzner/pkg/options"
	"github.com/spf13/cobra"
)

// createCmd represents the create command
var createCmd = &cobra.Command{
	Use:   "create",
	Short: "Create an instance",
	RunE:  createOrStartServer,
}

func createOrStartServer(cmd *cobra.Command, args []string) error {
	opts, err := options.FromEnv(false)
	if err != nil {
		return err
	}

	// Validate before making any API calls so bad input doesn't leave resources behind
	diskSize, err := opts.DiskSizeGB()
	if err != nil {
		return err
	}

	ctx := context.Background()
	h := hetzner.NewHetzner(opts.Token)

	req, publicKey, privateKey, err := h.BuildServerOptions(ctx, opts)
	if err != nil {
		return err
	}
	if publicKey == nil {
		return errors.New("no public key generated")
	}

	return h.Create(ctx, req, diskSize, *publicKey, privateKey)
}

func init() {
	rootCmd.AddCommand(createCmd)
}

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
	"fmt"
	"os"

	"github.com/FKint/devpod-provider-hetzner/pkg/hetzner"
	"github.com/FKint/devpod-provider-hetzner/pkg/options"
	"github.com/loft-sh/devpod/pkg/ssh"
	"github.com/loft-sh/log"
	"github.com/spf13/cobra"
)

// commandCmd represents the command command
var commandCmd = &cobra.Command{
	Use:   "command",
	Short: "Run a command on the instance",
	RunE: func(cmd *cobra.Command, args []string) error {
		opts, err := options.FromEnv(false)
		if err != nil {
			return err
		}

		ctx := context.Background()

		command := os.Getenv("COMMAND")
		if command == "" {
			return fmt.Errorf("command environment variable is missing")
		}

		// Get private key
		privateKey, err := ssh.GetPrivateKeyRawBase(opts.MachineFolder)
		if err != nil {
			return fmt.Errorf("load private key: %w", err)
		}

		// Create SSH client
		server, err := hetzner.NewHetzner(opts.Token).GetByName(ctx, opts.MachineID)
		if err != nil {
			return err
		} else if server == nil {
			return fmt.Errorf("vm not found")
		}

		// Call external address
		sshClient, err := hetzner.DialSSH(ctx, server.PublicNet.IPv4.IP.String(), privateKey)
		if err != nil {
			return fmt.Errorf("create ssh client: %w", err)
		}
		defer func() {
			if err := sshClient.Close(); err != nil {
				log.Default.Debugf("Error closing SSH connection: %v", err)
			}
		}()

		// Run command
		return ssh.Run(ctx, sshClient, command, os.Stdin, os.Stdout, os.Stderr, nil)
	},
}

func init() {
	rootCmd.AddCommand(commandCmd)
}

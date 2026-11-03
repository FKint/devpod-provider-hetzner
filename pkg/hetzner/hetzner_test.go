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

package hetzner

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"sigs.k8s.io/yaml"
)

func TestFingerPrintGenerate(t *testing.T) {
	tests := []struct {
		Name        string
		PublicKey   string
		Fingerprint string
		Error       error
	}{
		{
			Name: "rsa-1",
			//nolint
			PublicKey:   "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQDVEnA5bsxU1ltrt9mPho/JrVeMS17sI9GjIeNCLcb2bIFTzZ6I8d+hFddgmHFItgLJLJWUYDIHjhE0yB6zLKVkDmeQ/T4Qy2UaV2x8O+KQa+7Chl8DaTfnr/0b8flaFG9VSLJKA/QJ/Sl07oCbRQt3l9bHXvVMux0VTGavEjpKwtFFtWkDx/vDxJoFsA+oMkGaF2AP2+jIc3WCATaprllUxI42pav52m065fpPEvMfK8LJ3L6t5IOa49LieoNPz23s5GOsN66E6kmNuuWQ/HH7I0vPovoeHqizX9CkHTdTYuI87Je39yEjVliMQurEUouHlZU075P06SBYGnObp9yp",
			Fingerprint: "99:e0:3a:b1:44:4a:7b:d1:6e:d7:61:a1:5f:f8:ec:6c",
		},
		{
			Name: "rsa-2",
			//nolint
			PublicKey:   "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQDEAIu+Kqb3/3Lju+6r4DG7Vj36FtCf98wkWAcJECdvOde9QvBWLNC3butZZDUdu85ceQ0gRQrLXhLO8hwmf9ByRfUbsAiPR/xEMBKrYnHdaZEjwQMELGeoYpm3xQtcKHI5jRBdrR6jd0GLjwev8EDIJYmXF0Mu5GYR1aTadkKQBEPv52XcJgVS17HxI+L5s44xoqUedLUPBR2toj3ga7awzVDBRhlJRrShvmOso0AuOxRm1IfjtA1bsSgov2041v92d/xHURCfCLc6Nu/TEhKgx6DZk4flslMcRUdT5z/HeWfBtrjl0tTrJ6fIHffi/v9MsXXwnKe6dhUn5Ey10brN",
			Fingerprint: "20:98:ed:f5:86:af:db:0e:ea:99:97:12:88:ce:22:3b",
		},
		{
			Name:        "ed25519-1",
			PublicKey:   "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIMYMPf45N2zLPaI4SOxE4QJH/f4jhaLt7bSk75RVoIOA vscode@8422b61228f0",
			Fingerprint: "d4:dd:bf:79:27:15:d2:36:d1:13:60:79:6a:86:d8:7a",
		},
		{
			Name:      "error",
			PublicKey: "some invalid key",
			Error:     errors.New("ssh: no key found"),
		},
	}

	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			f, err := generateSSHKeyFingerprint(test.PublicKey)

			if test.Error == nil {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err, test.Error)
			}
			assert.Equal(t, f, test.Fingerprint)
		})
	}
}

func TestSSHKeyName(t *testing.T) {
	assert.Equal(t, "short-abcdefgh", sshKeyName("short", "ABCDEFGHIJKL"))

	long := "devpod-a-very-long-workspace-name-1234"
	name := sshKeyName(long, "ABCDEFGHIJKL")
	assert.Equal(t, long[:maxSSHKeyNameIDLength]+"-abcdefgh", name)
}

func TestCloudInitStatus(t *testing.T) {
	for out, wantDone := range map[string]bool{
		"status: done\n":          true,
		"status: degraded done\n": true,
		"status: running\n":       false,
		"status: error\n":         false,
	} {
		status, err := parseCloudInitStatus([]byte(out))
		assert.NoError(t, err)
		assert.Equal(t, wantDone, isCloudInitDone(status.Status), out)
	}

	assert.True(t, isCloudInitFailed("error"))
	assert.False(t, isCloudInitFailed("running"))
}

func TestGenerateUserData(t *testing.T) {
	buf, err := generateUserData("ssh-ed25519 AAAA test\n", 42)
	assert.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "scsi-0HC_Volume_42")
	assert.Contains(t, out, `- "ssh-ed25519 AAAA test"`)
	assert.Contains(t, out, "ufw --force enable")
}

func TestGenerateUserDataSSHDHardening(t *testing.T) {
	buf, err := generateUserData("ssh-ed25519 AAAA test", 1)
	assert.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "/etc/ssh/sshd_config.d/00-devpod.conf")
	assert.Contains(t, out, "systemctl restart ssh;")
	assert.Contains(t, out, "sed -i '1i PermitRootLogin no'")
}

func TestGenerateUserDataIsValidYAML(t *testing.T) {
	buf, err := generateUserData("ssh-ed25519 AAAA test", 1)
	assert.NoError(t, err)

	var cfg struct {
		RunCmd []any `json:"runcmd"`
	}
	assert.NoError(t, yaml.Unmarshal(buf.Bytes(), &cfg))
	assert.NotEmpty(t, cfg.RunCmd)
}

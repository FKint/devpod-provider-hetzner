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

package main

import (
	"testing"

	"github.com/loft-sh/devpod/pkg/types"
	"github.com/stretchr/testify/assert"
)

func TestCheckMachineTypeOffered(t *testing.T) {
	offered := types.OptionEnumArray{{Value: "cpx22"}, {Value: "cx33"}}

	assert.NoError(t, checkMachineTypeOffered("cx33", offered))

	err := checkMachineTypeOffered("cx32", offered)
	assert.ErrorContains(t, err, `"cx32"`)
	assert.ErrorContains(t, err, "MACHINE_TYPE")

	assert.Error(t, checkMachineTypeOffered("cx33", nil))
}

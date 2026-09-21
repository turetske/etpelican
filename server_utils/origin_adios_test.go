/***************************************************************
 *
 * Copyright (C) 2026, Pelican Project, Morgridge Institute for Research
 *
 * Licensed under the Apache License, Version 2.0 (the "License"); you
 * may not use this file except in compliance with the License.  You may
 * obtain a copy of the License at
 *
 *    http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 ***************************************************************/

package server_utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The ADIOS backend forwards paths verbatim; since ADIOS PR 5176 the
// server routes on an infix "_adios" marker, so the correct
// StoragePrefix is "/" (nothing prepended) and it must validate.
func TestADIOSOriginStoragePrefix(t *testing.T) {
	o := &ADIOSOrigin{}

	require.NoError(t, o.validateStoragePrefix("/"), "'/' is the deployed value")
	require.NoError(t, o.validateStoragePrefix("/adios"), "a real prefix is still allowed")

	assert.Error(t, o.validateStoragePrefix(""), "empty is rejected")
	assert.Error(t, o.validateStoragePrefix("adios"), "must begin with '/'")
	assert.Error(t, o.validateStoragePrefix("/a/../b"), "traversal is rejected")
}

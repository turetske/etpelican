/***************************************************************
 *
 * Copyright (C) 2024, Pelican Project, Morgridge Institute for Research
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

package broker

import (
	"testing"
	"time"

	"github.com/lestrrat-go/jwx/v2/jwt"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pelicanplatform/pelican/server_utils"
	"github.com/pelicanplatform/pelican/test_utils"
)

func TestGetCacheHostnameFromToken(t *testing.T) {
	server_utils.ResetTestState()
	test_utils.InitClient(t, nil)

	viper.Set("Federation.RegistryUrl", "https://your-registry.com")

	tok, err := jwt.NewBuilder().
		Issuer(`https://your-registry.com/api/v1.0/registry/caches/https://cache.com`).
		IssuedAt(time.Now()).
		Build()
	require.NoError(t, err)
	tokByte, err := jwt.Sign(tok, jwt.WithInsecureNoSignature())
	require.NoError(t, err)

	hostname, err := getCacheHostnameFromToken(tokByte)
	require.NoError(t, err)
	assert.Equal(t, "https://cache.com", hostname)
}

//go:build !windows

/***************************************************************
 *
 * Copyright (C) 2024, University of Nebraska-Lincoln
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

package client_test

import (
	"archive/tar"
	"context"
	_ "embed"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pelicanplatform/pelican/client"
	"github.com/pelicanplatform/pelican/config"
	"github.com/pelicanplatform/pelican/fed_test_utils"
	"github.com/pelicanplatform/pelican/param"
	"github.com/pelicanplatform/pelican/pelican_url"
	"github.com/pelicanplatform/pelican/server_utils"
	"github.com/pelicanplatform/pelican/test_utils"
	"github.com/pelicanplatform/pelican/token"
	"github.com/pelicanplatform/pelican/token_scopes"
)

var (
	//go:embed resources/both-auth.yml
	bothAuthOriginCfg string

	//go:embed resources/both-public.yml
	bothPublicOriginCfg string

	//go:embed resources/one-pub-one-auth.yml
	mixedAuthOriginCfg string

	//go:embed resources/pub-export-no-directread.yml
	pubExportNoDirectRead string

	//go:embed resources/pub-origin-no-directread.yml
	pubOriginNoDirectRead string

	//go:embed resources/test-https-origin.yml
	httpsOriginConfig string
)

// Helper function to get a temporary token file
// NOTE: when used make sure to call os.Remove() on the file
func getTempToken(t *testing.T) (tempToken *os.File, tkn string) {
	issuer, err := config.GetServerIssuerURL()
	require.NoError(t, err)

	// Create a token file
	tokenConfig := token.NewWLCGToken()
	tokenConfig.Lifetime = time.Minute
	tokenConfig.Issuer = issuer
	tokenConfig.Subject = "origin"
	tokenConfig.AddAudienceAny()

	scopes := []token_scopes.TokenScope{}
	readScope, err := token_scopes.Wlcg_Storage_Read.Path("/")
	assert.NoError(t, err)
	scopes = append(scopes, readScope)
	modScope, err := token_scopes.Wlcg_Storage_Modify.Path("/")
	assert.NoError(t, err)
	scopes = append(scopes, modScope)
	tokenConfig.AddScopes(scopes...)
	tkn, err = tokenConfig.CreateToken()
	assert.NoError(t, err)
	tmpTok := filepath.Join(t.TempDir(), "token")
	tempToken, err = os.OpenFile(tmpTok, os.O_CREATE|os.O_RDWR, 0644)
	assert.NoError(t, err, "Error opening the temp token file")
	_, err = tempToken.WriteString(tkn)
	assert.NoError(t, err, "Error writing to temp token file")

	return
}

// A test that spins up a federation, and tests object get and put
func TestGetAndPutAuth(t *testing.T) {
	server_utils.ResetTestState()

	fed := fed_test_utils.NewFedTest(t, bothAuthOriginCfg)
	discoveryUrl, err := url.Parse(param.Federation_DiscoveryUrl.GetString())
	assert.NoError(t, err)

	// Other set-up items:
	testFileContent := "test file content"
	// Create the temporary file to upload
	tempFile, err := os.CreateTemp(t.TempDir(), "test")
	assert.NoError(t, err, "Error creating temp file")
	defer os.Remove(tempFile.Name())
	_, err = tempFile.WriteString(testFileContent)
	assert.NoError(t, err, "Error writing to temp file")
	tempFile.Close()

	tempToken, tmpTkn := getTempToken(t)
	defer tempToken.Close()
	defer os.Remove(tempToken.Name())

	// Disable progress bars to not reuse the same mpb instance
	viper.Set("Logging.DisableProgressBars", true)

	// This tests object get/put with a pelican:// url
	t.Run("testPelicanObjectPutAndGetWithPelicanUrl", func(t *testing.T) {
		oldPref, err := config.SetPreferredPrefix(config.PelicanPrefix)
		defer func() {
			_, err := config.SetPreferredPrefix(oldPref)
			require.NoError(t, err)
		}()
		assert.NoError(t, err)

		// Set path for object to upload/download
		for _, export := range fed.Exports {
			tempPath := tempFile.Name()
			fileName := filepath.Base(tempPath)
			uploadURL := fmt.Sprintf("pelican://%s%s/%s/%s", discoveryUrl.Host,
				export.FederationPrefix, "osdf_osdf", fileName)
			// Upload the file with PUT
			transferResultsUpload, err := client.DoPut(fed.Ctx, tempFile.Name(), uploadURL, false, client.WithTokenLocation(tempToken.Name()))
			require.NoError(t, err)
			assert.Equal(t, transferResultsUpload[0].TransferredBytes, int64(17))

			// Download that same file with GET
			transferResultsDownload, err := client.DoGet(fed.Ctx, uploadURL, t.TempDir(), false, client.WithTokenLocation(tempToken.Name()))
			require.NoError(t, err)
			assert.Equal(t, transferResultsDownload[0].TransferredBytes, transferResultsUpload[0].TransferredBytes)
		}
	})

	t.Run("testPelicanObjectPutAndGetWithQueryAndDestDir", func(t *testing.T) {
		oldPref, err := config.SetPreferredPrefix(config.PelicanPrefix)
		defer func() {
			_, err := config.SetPreferredPrefix(oldPref)
			require.NoError(t, err)
		}()
		assert.NoError(t, err)

		// Set path for object to upload/download
		for _, export := range fed.Exports {
			tempPath := tempFile.Name()
			fileName := filepath.Base(tempPath)
			uploadUrlStr := fmt.Sprintf("%s/%s/%s",
				export.FederationPrefix, "osdf_osdf", fileName)
			uploadUrl, err := pelican_url.Parse(uploadUrlStr, nil, []pelican_url.DiscoveryOption{pelican_url.WithDiscoveryUrl(discoveryUrl)})
			assert.NoError(t, err)

			// Upload the file with PUT
			transferResultsUpload, err := client.DoPut(fed.Ctx, tempFile.Name(), uploadUrl.String(), false, client.WithTokenLocation(tempToken.Name()))
			require.NoError(t, err)
			require.Equal(t, transferResultsUpload[0].TransferredBytes, int64(17))

			queryUrl, err := pelican_url.Parse(uploadUrlStr+"?directread", nil, []pelican_url.DiscoveryOption{pelican_url.WithDiscoveryUrl(discoveryUrl)})
			assert.NoError(t, err)
			tempDir := t.TempDir()
			// Download that same file with GET
			transferResultsDownload, err := client.DoGet(fed.Ctx, queryUrl.String(), tempDir, false, client.WithTokenLocation(tempToken.Name()))

			require.NoError(t, err)
			require.Equal(t, transferResultsDownload[0].TransferredBytes, transferResultsUpload[0].TransferredBytes)

			stats, err := os.Stat(filepath.Join(tempDir, fileName))
			assert.NoError(t, err)
			assert.NotNil(t, stats)
		}
	})

	// We ran into a bug with the token option not working how it should. This test ensures that transfer option works how it should
	t.Run("testPelicanObjectPutAndGetWithWithTokenOption", func(t *testing.T) {
		oldPref, err := config.SetPreferredPrefix(config.PelicanPrefix)
		defer func() {
			_, err := config.SetPreferredPrefix(oldPref)
			require.NoError(t, err)
		}()
		assert.NoError(t, err)

		// Set path for object to upload/download
		for _, export := range fed.Exports {
			tempPath := tempFile.Name()
			fileName := filepath.Base(tempPath)
			uploadURL := fmt.Sprintf("pelican://%s:%s%s/%s/%s", param.Server_Hostname.GetString(), strconv.Itoa(param.Server_WebPort.GetInt()),
				export.FederationPrefix, "osdf_osdf", fileName)

			// Upload the file with PUT
			transferResultsUpload, err := client.DoPut(fed.Ctx, tempFile.Name(), uploadURL, false, client.WithToken(tmpTkn))
			require.NoError(t, err)
			require.Equal(t, len(transferResultsUpload), 1)
			require.Equal(t, transferResultsUpload[0].TransferredBytes, int64(17))

			// Download that same file with GET
			transferResultsDownload, err := client.DoGet(fed.Ctx, uploadURL, t.TempDir(), false, client.WithToken(tmpTkn))
			require.NoError(t, err)
			require.Equal(t, len(transferResultsDownload), 1)
			assert.Equal(t, transferResultsDownload[0].TransferredBytes, transferResultsUpload[0].TransferredBytes)
		}
	})

	// This tests object get/put with a pelican:// url
	t.Run("testOsdfObjectPutAndGetWithPelicanUrl", func(t *testing.T) {
		oldPref, err := config.SetPreferredPrefix(config.OsdfPrefix)
		assert.NoError(t, err)
		defer func() {
			_, err := config.SetPreferredPrefix(oldPref)
			require.NoError(t, err)
		}()

		for _, export := range fed.Exports {
			// Set path for object to upload/download
			tempPath := tempFile.Name()
			fileName := filepath.Base(tempPath)
			uploadURL := fmt.Sprintf("pelican://%s%s/%s/%s", discoveryUrl.Host,
				export.FederationPrefix, "osdf_osdf", fileName)

			// Upload the file with PUT
			transferResultsUpload, err := client.DoPut(fed.Ctx, tempFile.Name(), uploadURL, false, client.WithTokenLocation(tempToken.Name()))
			require.NoError(t, err)
			require.Equal(t, len(transferResultsUpload), 1)
			require.Equal(t, transferResultsUpload[0].TransferredBytes, int64(17))

			// Download that same file with GET
			transferResultsDownload, err := client.DoGet(fed.Ctx, uploadURL, t.TempDir(), false, client.WithTokenLocation(tempToken.Name()))
			require.NoError(t, err)
			assert.Equal(t, transferResultsDownload[0].TransferredBytes, transferResultsUpload[0].TransferredBytes)
		}
	})

	// This tests pelican object get/put with an osdf url
	t.Run("testOsdfObjectPutAndGetWithOSDFUrl", func(t *testing.T) {
		oldPref, err := config.SetPreferredPrefix(config.OsdfPrefix)
		assert.NoError(t, err)
		defer func() {
			_, err := config.SetPreferredPrefix(oldPref)
			require.NoError(t, err)
		}()

		oldHost, err := pelican_url.SetOsdfDiscoveryHost(discoveryUrl.String())
		require.NoError(t, err)
		defer func() {
			_, _ = pelican_url.SetOsdfDiscoveryHost(oldHost)
		}()

		for _, export := range fed.Exports {
			// Set path for object to upload/download
			tempPath := tempFile.Name()
			fileName := filepath.Base(tempPath)
			// Minimal fix of test as it is soon to be replaced
			uploadUrl := fmt.Sprintf("osdf://%s/%s", export.FederationPrefix, fileName)

			// Upload the file with PUT
			transferResultsUpload, err := client.DoPut(fed.Ctx, tempFile.Name(), uploadUrl, false, client.WithTokenLocation(tempToken.Name()))
			require.NoError(t, err)
			require.Equal(t, transferResultsUpload[0].TransferredBytes, int64(17))

			// Download that same file with GET
			transferResultsDownload, err := client.DoGet(fed.Ctx, uploadUrl, t.TempDir(), false, client.WithTokenLocation(tempToken.Name()))
			assert.NoError(t, err)
			assert.Equal(t, transferResultsDownload[0].TransferredBytes, transferResultsUpload[0].TransferredBytes)
		}
	})
	t.Cleanup(func() {
		// Throw in a config.Reset for good measure. Keeps our env squeaky clean!
		server_utils.ResetTestState()
	})
}

// A test that spins up a federation, and tests object get and put
func TestCopyAuth(t *testing.T) {
	server_utils.ResetTestState()

	fed := fed_test_utils.NewFedTest(t, bothAuthOriginCfg)
	discoveryUrl, err := url.Parse(param.Federation_DiscoveryUrl.GetString())
	assert.NoError(t, err)

	te, err := client.NewTransferEngine(fed.Ctx)
	require.NoError(t, err)

	// Other set-up items:
	testFileContent := "test file content"
	// Create the temporary file to upload
	tempFile, err := os.CreateTemp(t.TempDir(), "test")
	assert.NoError(t, err, "Error creating temp file")
	defer os.Remove(tempFile.Name())
	_, err = tempFile.WriteString(testFileContent)
	assert.NoError(t, err, "Error writing to temp file")
	tempFile.Close()

	tempToken, _ := getTempToken(t)
	defer tempToken.Close()
	defer os.Remove(tempToken.Name())
	// Disable progress bars to not reuse the same mpb instance
	viper.Set("Logging.DisableProgressBars", true)

	// This tests object get/put with a pelican:// url
	t.Run("testPelicanObjectCopyWithPelicanUrl", func(t *testing.T) {
		oldPref, err := config.SetPreferredPrefix(config.PelicanPrefix)
		assert.NoError(t, err)
		defer func() {
			_, err := config.SetPreferredPrefix(oldPref)
			require.NoError(t, err)
		}()

		// Set path for object to upload/download
		for _, export := range fed.Exports {
			tempPath := tempFile.Name()
			fileName := filepath.Base(tempPath)
			uploadURL := fmt.Sprintf("pelican://%s%s/%s/%s", discoveryUrl.Host,
				export.FederationPrefix, "osdf_osdf", fileName)

			// Upload the file with COPY
			transferResultsUpload, err := client.DoCopy(fed.Ctx, tempFile.Name(), uploadURL, false, client.WithTokenLocation(tempToken.Name()))
			assert.NoError(t, err)
			assert.Equal(t, int64(17), transferResultsUpload[0].TransferredBytes)

			// Download that same file with COPY
			transferResultsDownload, err := client.DoCopy(fed.Ctx, uploadURL, t.TempDir(), false, client.WithTokenLocation(tempToken.Name()))
			assert.NoError(t, err)
			assert.Equal(t, int64(17), transferResultsDownload[0].TransferredBytes)
		}
	})

	t.Run("testPelicanObjectCopyWithQueryAndDestDir", func(t *testing.T) {
		oldPref, err := config.SetPreferredPrefix(config.PelicanPrefix)
		assert.NoError(t, err)
		defer func() {
			_, err := config.SetPreferredPrefix(oldPref)
			require.NoError(t, err)
		}()

		// Set path for object to upload/download
		for _, export := range fed.Exports {
			tempPath := tempFile.Name()
			fileName := filepath.Base(tempPath)
			uploadUrlStr := fmt.Sprintf("%s/%s/%s",
				export.FederationPrefix, "osdf_osdf", fileName)

			uploadUrl, err := pelican_url.Parse(uploadUrlStr, nil, []pelican_url.DiscoveryOption{pelican_url.WithDiscoveryUrl(discoveryUrl)})
			assert.NoError(t, err)

			// Upload the file with COPY
			transferResultsUpload, err := client.DoCopy(fed.Ctx, tempFile.Name(), uploadUrl.String(), false, client.WithTokenLocation(tempToken.Name()))
			assert.NoError(t, err)
			assert.Equal(t, int64(17), transferResultsUpload[0].TransferredBytes)

			queryUrl, err := pelican_url.Parse(uploadUrlStr+"?directread", nil, []pelican_url.DiscoveryOption{pelican_url.WithDiscoveryUrl(discoveryUrl)})
			assert.NoError(t, err)
			tempDir := t.TempDir()
			// Download that same file with COPY
			transferResultsDownload, err := client.DoCopy(fed.Ctx, queryUrl.String(), tempDir, false, client.WithTokenLocation(tempToken.Name()))
			assert.NoError(t, err)
			assert.Equal(t, int64(17), transferResultsDownload[0].TransferredBytes)
			stats, err := os.Stat(filepath.Join(tempDir, fileName))
			assert.NoError(t, err)
			assert.NotNil(t, stats)
		}
	})

	// This tests object get/put with a pelican:// url
	t.Run("testOsdfObjectCopyWithPelicanUrl", func(t *testing.T) {
		oldPref, err := config.SetPreferredPrefix(config.OsdfPrefix)
		assert.NoError(t, err)
		defer func() {
			_, err := config.SetPreferredPrefix(oldPref)
			require.NoError(t, err)
		}()

		for _, export := range fed.Exports {
			// Set path for object to upload/download
			tempPath := tempFile.Name()
			fileName := filepath.Base(tempPath)
			uploadURL := fmt.Sprintf("pelican://%s%s/%s/%s", discoveryUrl.Host,
				export.FederationPrefix, "osdf_osdf", fileName)

			// Upload the file with PUT
			transferResultsUpload, err := client.DoCopy(fed.Ctx, tempFile.Name(), uploadURL, false, client.WithTokenLocation(tempToken.Name()))
			assert.NoError(t, err)
			assert.Equal(t, transferResultsUpload[0].TransferredBytes, int64(17))

			// Download that same file with GET
			transferResultsDownload, err := client.DoCopy(fed.Ctx, uploadURL, t.TempDir(), false, client.WithTokenLocation(tempToken.Name()))
			assert.NoError(t, err)
			assert.Equal(t, transferResultsDownload[0].TransferredBytes, transferResultsUpload[0].TransferredBytes)
		}
	})

	// This tests pelican object get/put with an osdf url
	t.Run("testOsdfObjectCopyWithOSDFUrl", func(t *testing.T) {
		oldPref, err := config.SetPreferredPrefix(config.OsdfPrefix)
		assert.NoError(t, err)
		defer func() {
			_, err := config.SetPreferredPrefix(oldPref)
			require.NoError(t, err)
		}()

		oldHost, err := pelican_url.SetOsdfDiscoveryHost(discoveryUrl.String())
		require.NoError(t, err)
		defer func() {
			_, _ = pelican_url.SetOsdfDiscoveryHost(oldHost)
		}()

		for _, export := range fed.Exports {
			// Set path for object to upload/download
			tempPath := tempFile.Name()
			fileName := filepath.Base(tempPath)
			// Minimal fix of test as it is soon to be replaced
			uploadUrl := fmt.Sprintf("osdf://%s/%s", export.FederationPrefix, fileName)

			// Upload the file with PUT
			transferResultsUpload, err := client.DoCopy(fed.Ctx, tempFile.Name(), uploadUrl, false, client.WithTokenLocation(tempToken.Name()))
			assert.NoError(t, err)
			assert.Equal(t, transferResultsUpload[0].TransferredBytes, int64(17))

			// Download that same file with GET
			transferResultsDownload, err := client.DoCopy(fed.Ctx, uploadUrl, t.TempDir(), false, client.WithTokenLocation(tempToken.Name()))
			assert.NoError(t, err)
			assert.Equal(t, transferResultsDownload[0].TransferredBytes, transferResultsUpload[0].TransferredBytes)
		}
	})
	t.Cleanup(func() {
		if err := te.Shutdown(); err != nil {
			log.Errorln("Failure when shutting down transfer engine:", err)
		}
		// Throw in a config.Reset for good measure. Keeps our env squeaky clean!
		server_utils.ResetTestState()
	})
}

// A test that spins up the federation, where the origin is in EnablePublicReads mode. Then GET a file from the origin without a token
func TestGetPublicRead(t *testing.T) {
	server_utils.ResetTestState()

	fed := fed_test_utils.NewFedTest(t, bothPublicOriginCfg)

	t.Run("testPubObjGet", func(t *testing.T) {
		for _, export := range fed.Exports {
			testFileContent := "test file content"
			// Drop the testFileContent into the origin directory
			tempFile, err := os.Create(filepath.Join(export.StoragePrefix, "test.txt"))
			assert.NoError(t, err, "Error creating temp file")
			defer os.Remove(tempFile.Name())
			_, err = tempFile.WriteString(testFileContent)
			assert.NoError(t, err, "Error writing to temp file")
			tempFile.Close()

			viper.Set("Logging.DisableProgressBars", true)

			// Set path for object to upload/download
			tempPath := tempFile.Name()
			fileName := filepath.Base(tempPath)
			uploadURL := fmt.Sprintf("pelican://%s:%s%s/%s", param.Server_Hostname.GetString(), strconv.Itoa(param.Server_WebPort.GetInt()),
				export.FederationPrefix, fileName)

			// Download the file with GET. Shouldn't need a token to succeed
			transferResults, err := client.DoGet(fed.Ctx, uploadURL, t.TempDir(), false)
			assert.NoError(t, err)
			assert.Equal(t, transferResults[0].TransferredBytes, int64(17))
		}
	})
	t.Cleanup(func() {
		// Throw in a config.Reset for good measure. Keeps our env squeaky clean!
		server_utils.ResetTestState()
	})
}

// A test that spins up a federation, and tests object stat
func TestObjectStat(t *testing.T) {
	server_utils.ResetTestState()

	defer server_utils.ResetTestState()
	fed := fed_test_utils.NewFedTest(t, mixedAuthOriginCfg)
	discoveryUrl, err := url.Parse(param.Federation_DiscoveryUrl.GetString())
	require.NoError(t, err)

	// Other set-up items:
	testFileContent := "test file content"
	// Create the temporary file to upload
	tempFileName := filepath.Join(t.TempDir(), "test")
	tempFile, err := os.OpenFile(tempFileName, os.O_CREATE|os.O_RDWR, 0644)
	assert.NoError(t, err, "Error creating temp file")
	_, err = tempFile.WriteString(testFileContent)
	assert.NoError(t, err, "Error writing to temp file")
	tempFile.Close()

	// Get a temporary token file
	tempToken, _ := getTempToken(t)
	defer tempToken.Close()
	defer os.Remove(tempToken.Name())

	// Disable progress bars to not reuse the same mpb instance
	viper.Set("Logging.DisableProgressBars", true)

	// Make directories for test within origin exports
	destDir1 := filepath.Join(fed.Exports[0].StoragePrefix, "test")
	require.NoError(t, os.MkdirAll(destDir1, os.FileMode(0755)))
	destDir2 := filepath.Join(fed.Exports[1].StoragePrefix, "test")
	require.NoError(t, os.MkdirAll(destDir2, os.FileMode(0755)))

	// This tests object stat with no flags set
	t.Run("testPelicanObjectStatNoFlags", func(t *testing.T) {
		for _, export := range fed.Exports {
			statUrl := fmt.Sprintf("pelican://%s%s/hello_world.txt", discoveryUrl.Host, export.FederationPrefix)
			var got client.FileInfo
			if export.Capabilities.PublicReads {
				statInfo, err := client.DoStat(fed.Ctx, statUrl, client.WithTokenLocation(""))
				require.NoError(t, err)
				got = *statInfo
			} else {
				statInfo, err := client.DoStat(fed.Ctx, statUrl, client.WithTokenLocation(tempToken.Name()))
				require.NoError(t, err)
				got = *statInfo
			}
			assert.Equal(t, int64(13), got.Size)
			assert.Equal(t, fmt.Sprintf("%s/hello_world.txt", export.FederationPrefix), got.Name)
			assert.Nil(t, got.Checksums)

			// Repeat the process with checksum requests
			if export.Capabilities.PublicReads {
				statInfo, err := client.DoStat(fed.Ctx, statUrl, client.WithTokenLocation(""), client.WithRequestChecksums([]client.ChecksumType{client.AlgCRC32C}))
				require.NoError(t, err)
				got = *statInfo
			} else {
				statInfo, err := client.DoStat(fed.Ctx, statUrl, client.WithTokenLocation(tempToken.Name()), client.WithRequestChecksums([]client.ChecksumType{client.AlgCRC32C}))
				require.NoError(t, err)
				got = *statInfo
			}
			assert.Equal(t, int64(13), got.Size)
			assert.Equal(t, fmt.Sprintf("%s/hello_world.txt", export.FederationPrefix), got.Name)
			assert.NotNil(t, got.Checksums)
			val, ok := got.Checksums["crc32c"]
			assert.True(t, ok)
			assert.Equal(t, "4d551068", val)
		}
	})

	// This tests object stat when used on a directory
	t.Run("testPelicanObjectStatOnDirectory", func(t *testing.T) {
		for _, export := range fed.Exports {
			statUrl := fmt.Sprintf("pelican://%s%s/test", discoveryUrl.Host, export.FederationPrefix)
			if export.Capabilities.PublicReads {
				statInfo, err := client.DoStat(fed.Ctx, statUrl, client.WithTokenLocation(""))
				require.NoError(t, err)
				assert.Equal(t, int64(0), statInfo.Size)
			} else {
				statInfo, err := client.DoStat(fed.Ctx, statUrl, client.WithTokenLocation(tempToken.Name()))
				require.NoError(t, err)
				assert.Equal(t, int64(0), statInfo.Size)
			}
		}
	})

	// Ensure stat works with an OSDF scheme
	t.Run("testObjectStatOSDFScheme", func(t *testing.T) {
		oldPref, err := config.SetPreferredPrefix(config.OsdfPrefix)
		assert.NoError(t, err)
		defer func() {
			_, err := config.SetPreferredPrefix(oldPref)
			require.NoError(t, err)
		}()

		oldHost, err := pelican_url.SetOsdfDiscoveryHost(discoveryUrl.String())
		require.NoError(t, err)
		defer func() {
			_, _ = pelican_url.SetOsdfDiscoveryHost(oldHost)
		}()

		testFileContent := "test file content"
		// Drop the testFileContent into the origin directory
		tempFile, err := os.Create(filepath.Join(fed.Exports[0].StoragePrefix, "test.txt"))
		assert.NoError(t, err, "Error creating temp file")
		_, err = tempFile.WriteString(testFileContent)
		assert.NoError(t, err, "Error writing to temp file")
		tempFile.Close()

		viper.Set("Logging.DisableProgressBars", true)

		tempPath := tempFile.Name()
		fileName := filepath.Base(tempPath)
		statUrl := fmt.Sprintf("osdf://%s/%s", fed.Exports[0].FederationPrefix, fileName)

		// Stat the file
		statInfo, err := client.DoStat(fed.Ctx, statUrl)
		assert.NoError(t, err)
		assert.Equal(t, int64(17), int64(statInfo.Size))
		assert.Equal(t, fmt.Sprintf("%s/%s", fed.Exports[0].FederationPrefix, fileName), statInfo.Name)
	})
}

// Test the functionality of the direct reads feature (?directread)
func TestDirectReads(t *testing.T) {
	defer server_utils.ResetTestState()
	t.Run("testDirectReadsSuccess", func(t *testing.T) {
		server_utils.ResetTestState()

		viper.Set("Origin.EnableDirectReads", true)
		fed := fed_test_utils.NewFedTest(t, bothPublicOriginCfg)
		discoveryUrl, err := url.Parse(param.Federation_DiscoveryUrl.GetString())
		require.NoError(t, err)
		export := fed.Exports[0]
		testFileContent := "test file content"
		// Drop the testFileContent into the origin directory
		tempFile, err := os.Create(filepath.Join(export.StoragePrefix, "test.txt"))
		assert.NoError(t, err, "Error creating temp file")
		defer os.Remove(tempFile.Name())
		_, err = tempFile.WriteString(testFileContent)
		assert.NoError(t, err, "Error writing to temp file")
		tempFile.Close()

		viper.Set("Logging.DisableProgressBars", true)

		// Set path for object to upload/download
		tempPath := tempFile.Name()
		fileName := filepath.Base(tempPath)
		uploadURL := fmt.Sprintf("pelican://%s%s/%s?directread", discoveryUrl.Host, export.FederationPrefix, fileName)

		// Download the file with GET. Shouldn't need a token to succeed
		transferResults, err := client.DoGet(fed.Ctx, uploadURL, t.TempDir(), false)
		require.NoError(t, err)
		assert.Equal(t, transferResults[0].TransferredBytes, int64(17))

		// Assert that the file was not cached
		cacheDataLocation := param.Cache_StorageLocation.GetString() + export.FederationPrefix
		filepath := filepath.Join(cacheDataLocation, filepath.Base(tempFile.Name()))
		_, err = os.Stat(filepath)
		assert.True(t, os.IsNotExist(err))

		// Assert our endpoint was the origin and not the cache
		for _, attempt := range transferResults[0].Attempts {
			assert.Equal(t, "https://"+attempt.Endpoint, param.Origin_Url.GetString())
		}
	})

	// Test that direct reads fail if DirectReads=false is set for origin config but true for namespace/export
	t.Run("testDirectReadsDirectReadFalseByOrigin", func(t *testing.T) {
		server_utils.ResetTestState()

		fed := fed_test_utils.NewFedTest(t, pubOriginNoDirectRead)
		discoveryUrl, err := url.Parse(param.Federation_DiscoveryUrl.GetString())
		require.NoError(t, err)
		export := fed.Exports[0]
		testFileContent := "test file content"
		// Drop the testFileContent into the origin directory
		tempFile, err := os.Create(filepath.Join(export.StoragePrefix, "test.txt"))
		assert.NoError(t, err, "Error creating temp file")
		defer os.Remove(tempFile.Name())
		_, err = tempFile.WriteString(testFileContent)
		assert.NoError(t, err, "Error writing to temp file")
		tempFile.Close()

		viper.Set("Logging.DisableProgressBars", true)

		// Set path for object to upload/download
		tempPath := tempFile.Name()
		fileName := filepath.Base(tempPath)
		uploadURL := fmt.Sprintf("pelican://%s%s/%s?directread", discoveryUrl.Host, export.FederationPrefix, fileName)

		// Download the file with GET. Shouldn't need a token to succeed
		_, err = client.DoGet(fed.Ctx, uploadURL, t.TempDir(), false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), fmt.Sprintf("%d", http.StatusMethodNotAllowed))
	})

	// Test that direct reads fail if DirectReads=false is set for namespace/export config but true for origin
	t.Run("testDirectReadsDirectReadFalseByNamespace", func(t *testing.T) {
		server_utils.ResetTestState()

		fed := fed_test_utils.NewFedTest(t, pubExportNoDirectRead)
		discoveryUrl, err := url.Parse(param.Federation_DiscoveryUrl.GetString())
		require.NoError(t, err)
		export := fed.Exports[0]
		export.Capabilities.DirectReads = false
		testFileContent := "test file content"
		// Drop the testFileContent into the origin directory
		tempFile, err := os.Create(filepath.Join(export.StoragePrefix, "test.txt"))
		assert.NoError(t, err, "Error creating temp file")
		defer os.Remove(tempFile.Name())
		_, err = tempFile.WriteString(testFileContent)
		assert.NoError(t, err, "Error writing to temp file")
		tempFile.Close()

		viper.Set("Logging.DisableProgressBars", true)

		// Set path for object to upload/download
		tempPath := tempFile.Name()
		fileName := filepath.Base(tempPath)
		uploadURL := fmt.Sprintf("pelican://%s%s/%s?directread", discoveryUrl.Host, export.FederationPrefix, fileName)

		// Download the file with GET. Shouldn't need a token to succeed
		_, err = client.DoGet(fed.Ctx, uploadURL, t.TempDir(), false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), fmt.Sprintf("%d", http.StatusMethodNotAllowed))
	})
}

// Test the functionality of NewTransferJob, checking we return at the correct locations for certain errors
func TestNewTransferJob(t *testing.T) {
	server_utils.ResetTestState()
	defer server_utils.ResetTestState()

	fed := fed_test_utils.NewFedTest(t, mixedAuthOriginCfg)
	discoveryUrl, err := url.Parse(param.Federation_DiscoveryUrl.GetString())
	require.NoError(t, err)
	te, err := client.NewTransferEngine(fed.Ctx)
	require.NoError(t, err)

	// Test when we have a failure during namespace lookup (here we will get a 404)
	t.Run("testFailureToGetNamespaceInfo", func(t *testing.T) {
		tc, err := te.NewClient()
		assert.NoError(t, err)

		// have a file/namespace that does not exist
		mockRemoteUrl, err := url.Parse(fmt.Sprintf("pelican://%s/first/something/file.txt", discoveryUrl.Host))
		require.NoError(t, err)
		_, err = tc.NewTransferJob(context.Background(), mockRemoteUrl, "/dest", false, false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get namespace information for remote URL")
	})

	// Test when we fail to get a token on our auth required namespace
	t.Run("testFailureToGetToken", func(t *testing.T) {
		tc, err := te.NewClient()
		assert.NoError(t, err)

		// use our auth required namespace
		mockRemoteUrl, err := url.Parse(fmt.Sprintf("pelican://%s/second/namespace/hello_world.txt", discoveryUrl.Host))
		require.NoError(t, err)
		_, err = tc.NewTransferJob(context.Background(), mockRemoteUrl, "/dest", false, false, client.WithAcquireToken(false))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get token for transfer: credential is required for")
	})

	// Test success
	t.Run("testSuccess", func(t *testing.T) {
		tc, err := te.NewClient()
		assert.NoError(t, err)
		remoteUrl, err := url.Parse(fmt.Sprintf("pelican://%s/first/namespace/hello_world.txt", discoveryUrl.Host))
		require.NoError(t, err)
		_, err = tc.NewTransferJob(context.Background(), remoteUrl, t.TempDir(), false, false)
		assert.NoError(t, err)
	})
}

// A test that spins up a federation, and tests object list
func TestObjectList(t *testing.T) {
	server_utils.ResetTestState()

	defer server_utils.ResetTestState()
	fed := fed_test_utils.NewFedTest(t, mixedAuthOriginCfg)

	// Other set-up items:
	testFileContent := "test file content"
	// Create the temporary file to upload
	tempFileName := filepath.Join(t.TempDir(), "test")
	tempFile, err := os.OpenFile(tempFileName, os.O_CREATE|os.O_RDWR, 0644)
	assert.NoError(t, err, "Error creating temp file")
	_, err = tempFile.WriteString(testFileContent)
	assert.NoError(t, err, "Error writing to temp file")
	tempFile.Close()

	// Get a temporary token file
	tempToken, _ := getTempToken(t)
	defer tempToken.Close()
	defer os.Remove(tempToken.Name())

	// Disable progress bars to not reuse the same mpb instance
	viper.Set("Logging.DisableProgressBars", true)

	// Make directories for test within origin exports
	destDir1 := filepath.Join(fed.Exports[0].StoragePrefix, "test")
	require.NoError(t, os.MkdirAll(destDir1, os.FileMode(0755)))
	destDir2 := filepath.Join(fed.Exports[1].StoragePrefix, "test")
	require.NoError(t, os.MkdirAll(destDir2, os.FileMode(0755)))

	// This tests object ls with no flags set
	t.Run("testPelicanObjectLsNoFlags", func(t *testing.T) {
		for _, export := range fed.Exports {
			listURL := fmt.Sprintf("pelican://%s:%d%s", param.Server_Hostname.GetString(), param.Server_WebPort.GetInt(), export.FederationPrefix)
			if export.Capabilities.PublicReads {
				get, err := client.DoList(fed.Ctx, listURL, client.WithTokenLocation(""))
				require.NoError(t, err)
				require.Len(t, get, 2)
				var name string
				if strings.Contains(get[0].Name, "hello_world.txt") {
					name = get[0].Name
				} else {
					name = get[1].Name
				}
				require.Equal(t, fmt.Sprintf("%s/hello_world.txt", export.FederationPrefix), name)
			} else {
				get, err := client.DoList(fed.Ctx, listURL, client.WithTokenLocation(tempToken.Name()))
				require.NoError(t, err)
				require.Len(t, get, 2)
				var name string
				if strings.Contains(get[0].Name, "hello_world.txt") {
					name = get[0].Name
				} else {
					name = get[1].Name
				}
				require.Equal(t, fmt.Sprintf("%s/hello_world.txt", export.FederationPrefix), name)
			}
		}
	})

	t.Run("testPelicanObjectLsNoTokForProtectedNs", func(t *testing.T) {
		for _, export := range fed.Exports {
			listURL := fmt.Sprintf("pelican://%s:%d%s", param.Server_Hostname.GetString(), param.Server_WebPort.GetInt(), export.FederationPrefix)
			if !export.Capabilities.PublicReads {
				get, err := client.DoList(fed.Ctx, listURL, client.WithTokenLocation(""), client.WithAcquireToken(false))
				require.Error(t, err)
				assert.Len(t, get, 0)
				assert.Contains(t, err.Error(), "failed to get token for transfer: credential is required")

				// No error if it's with token
				get, err = client.DoList(fed.Ctx, listURL, client.WithTokenLocation(tempToken.Name()), client.WithAcquireToken(false))
				require.NoError(t, err)
				require.Len(t, get, 2)
			} else {
				get, err := client.DoList(fed.Ctx, listURL, client.WithTokenLocation(tempToken.Name()), client.WithAcquireToken(false))
				require.NoError(t, err)
				require.Len(t, get, 2)
			}
		}
	})

	// Test we fail when we have an incorrect namespace
	t.Run("testPelicanObjectLsFailWhenNamespaceIncorrect", func(t *testing.T) {
		// set the prefix to /first instead of /first/namespace
		federationPrefix := "/first/"
		listURL := fmt.Sprintf("pelican://%s:%s%s", param.Server_Hostname.GetString(), strconv.Itoa(param.Server_WebPort.GetInt()), federationPrefix)

		_, err := client.DoList(fed.Ctx, listURL, nil, client.WithTokenLocation(tempToken.Name()))
		require.Error(t, err)
		require.Contains(t, err.Error(), fmt.Sprintf("%d", http.StatusNotFound))
	})
}

// This tests object ls but for an origin that supports listings but with an object store that does not support PROPFIND.
// We should get a 405 returned. This is a separate test since we need a completely different origin
func TestObjectList405Error(t *testing.T) {
	server_utils.ResetTestState()
	defer server_utils.ResetTestState()
	test_utils.InitClient(t, nil)

	var storageName string

	// Set up our http backend so that we can return a 405 on a PROPFIND
	body := "Hello, World!"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "HEAD" && r.URL.Path == storageName {
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
			w.WriteHeader(http.StatusOK)
			return
		} else if r.Method == "GET" && r.URL.Path == storageName {
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
			w.WriteHeader(http.StatusPartialContent)
			_, err := w.Write([]byte(body))
			require.NoError(t, err)
			return
		} else if r.Method == "PROPFIND" && r.URL.Path == storageName {
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer srv.Close()
	viper.Set("Origin.HttpServiceUrl", srv.URL)

	fed := fed_test_utils.NewFedTest(t, httpsOriginConfig)
	storageName = fed.Exports[0].StoragePrefix + "/hello_world"
	discoveryHost := param.Server_Hostname.GetString() + ":" + strconv.Itoa(param.Server_WebPort.GetInt())

	_, err := client.DoList(fed.Ctx, "pelican://"+discoveryHost+"/my-prefix/hello_world")
	require.Error(t, err)
	require.Contains(t, err.Error(), "405: object listings are not supported by the discovered origin")
}

// Startup a mini-federation and ensure the "pack=auto" functionality works
// end-to-end
func TestClientUnpack(t *testing.T) {
	server_utils.ResetTestState()
	test_utils.InitClient(t, nil)

	fed := fed_test_utils.NewFedTest(t, bothPublicOriginCfg)
	export := fed.Exports[0]

	tmpDir := t.TempDir()
	fooLocation := filepath.Join(tmpDir, "foo.txt")
	err := os.WriteFile(fooLocation, []byte("hello world"), os.FileMode(0600))
	require.NoError(t, err)
	fi, err := os.Stat(fooLocation)
	require.NoError(t, err)
	sourceTarLocation := filepath.Join(export.StoragePrefix, "testfile.tar")
	fd, err := os.OpenFile(sourceTarLocation, os.O_CREATE|os.O_WRONLY, os.FileMode(0644))
	require.NoError(t, err)
	defer func() {
		err := fd.Close()
		require.NoError(t, err)
	}()
	tf := tar.NewWriter(fd)
	hdr, err := tar.FileInfoHeader(fi, "")
	require.NoError(t, err)
	err = tf.WriteHeader(hdr)
	require.NoError(t, err)
	err = os.Remove(fooLocation)
	require.NoError(t, err)
	_, err = os.Stat(fooLocation)
	require.Error(t, err) // Double-check the file is gone

	_, err = tf.Write([]byte("hello world"))
	require.NoError(t, err)
	err = tf.Close()
	require.NoError(t, err)
	fi, err = os.Stat(sourceTarLocation)
	require.NoError(t, err)
	tarSize := fi.Size()

	downloadURL := fmt.Sprintf("pelican://%s:%s%s/testfile.tar?pack=auto&directread",
		param.Server_Hostname.GetString(),
		strconv.Itoa(param.Server_WebPort.GetInt()),
		export.FederationPrefix,
	)
	results, err := client.DoGet(fed.Ctx, downloadURL, tmpDir, false)
	require.NoError(t, err)
	require.Equal(t, 1, len(results))
	require.NoError(t, results[0].Error)
	assert.Equal(t, tarSize, results[0].TransferredBytes)

	// If the file was automatically unpacked, then instead of downloading something named
	// "testfile.tar", we should only have a file named "foo.txt"
	fi, err = os.Stat(fooLocation)
	require.NoError(t, err)
	assert.Equal(t, int64(11), fi.Size())
}

// A test that generates a token locally from the private key
func TestTokenGenerate(t *testing.T) {
	server_utils.ResetTestState()

	fed := fed_test_utils.NewFedTest(t, bothAuthOriginCfg)

	// Other set-up items:
	testFileContent := "test file content"
	// Create the temporary file to upload
	tempFile, err := os.CreateTemp(t.TempDir(), "test")
	require.NoError(t, err, "Error creating temp file")
	defer os.Remove(tempFile.Name())
	_, err = tempFile.WriteString(testFileContent)
	require.NoError(t, err, "Error writing to temp file")
	tempFile.Close()

	// Disable progress bars to not reuse the same mpb instance
	viper.Set("Logging.DisableProgressBars", true)

	// Set path for object to upload/download
	for _, export := range fed.Exports {
		tempPath := tempFile.Name()
		fileName := filepath.Base(tempPath)
		uploadURL := fmt.Sprintf("pelican://%s:%s%s/%s/%s", param.Server_Hostname.GetString(), strconv.Itoa(param.Server_WebPort.GetInt()),
			export.FederationPrefix, "token_gen", fileName)

		// Upload the file with PUT
		transferResultsUpload, err := client.DoPut(fed.Ctx, tempFile.Name(), uploadURL, false)
		require.NoError(t, err)
		assert.Equal(t, transferResultsUpload[0].TransferredBytes, int64(17))

		// Download that same file with GET
		transferResultsDownload, err := client.DoGet(fed.Ctx, uploadURL, t.TempDir(), false)
		require.NoError(t, err)
		assert.Equal(t, transferResultsDownload[0].TransferredBytes, transferResultsUpload[0].TransferredBytes)
	}
}

func TestPrestage(t *testing.T) {
	server_utils.ResetTestState()
	defer server_utils.ResetTestState()
	fed := fed_test_utils.NewFedTest(t, bothAuthOriginCfg)

	te, err := client.NewTransferEngine(fed.Ctx)
	require.NoError(t, err)

	// Other set-up items:
	// The cache will open the file to stat it, downloading the first block.
	// Make sure we are greater than 64kb in size.
	testFileContent := strings.Repeat("test file content", 10000)
	// Create the temporary file to upload
	tempFile, err := os.CreateTemp(t.TempDir(), "test")
	assert.NoError(t, err, "Error creating temp file")
	defer os.Remove(tempFile.Name())
	_, err = tempFile.WriteString(testFileContent)
	assert.NoError(t, err, "Error writing to temp file")
	tempFile.Close()

	tempToken, _ := getTempToken(t)
	defer tempToken.Close()
	defer os.Remove(tempToken.Name())
	// Disable progress bars to not reuse the same mpb instance
	viper.Set("Logging.DisableProgressBars", true)

	oldPref, err := config.SetPreferredPrefix(config.PelicanPrefix)
	assert.NoError(t, err)
	defer func() {
		_, err := config.SetPreferredPrefix(oldPref)
		require.NoError(t, err)
	}()

	// Set path for object to upload/download
	for _, export := range fed.Exports {
		tempPath := tempFile.Name()
		fileName := filepath.Base(tempPath)
		uploadURL := fmt.Sprintf("pelican://%s:%s%s/prestage/%s", param.Server_Hostname.GetString(), strconv.Itoa(param.Server_WebPort.GetInt()),
			export.FederationPrefix, fileName)

		// Upload the file with COPY
		transferResultsUpload, err := client.DoCopy(fed.Ctx, tempFile.Name(), uploadURL, false, client.WithTokenLocation(tempToken.Name()))
		assert.NoError(t, err)
		assert.Equal(t, int64(len(testFileContent)), transferResultsUpload[0].TransferredBytes)

		// Check the cache info twice, make sure it's not cached.
		tc, err := te.NewClient(client.WithTokenLocation(tempToken.Name()))
		require.NoError(t, err)
		innerFileUrl, err := url.Parse(uploadURL)
		require.NoError(t, err)
		age, size, err := tc.CacheInfo(fed.Ctx, innerFileUrl)
		require.NoError(t, err)
		require.Equal(t, int64(len(testFileContent)), size)
		// Due to an xrootd limitation, CacheInfo performs a GET request instead of a HEAD request.
		// Once this limitation is resolved and CacheInfo is updated accordingly.
		if age != -1 && age != 0 {
			require.Fail(t, "CacheInfo age should be -1 or 0, but got %d", age)
		}

		age, size, err = tc.CacheInfo(fed.Ctx, innerFileUrl)
		require.NoError(t, err)
		assert.Equal(t, int64(len(testFileContent)), size)
		// Due to an xrootd limitation, CacheInfo performs a GET request instead of a HEAD request.
		// Once this limitation is resolved and CacheInfo is updated accordingly.
		if age != -1 && age != 0 {
			require.Fail(t, "CacheInfo age should be -1 or 0, but got %d", age)
		}

		// Prestage the object
		tj, err := tc.NewPrestageJob(fed.Ctx, innerFileUrl)
		require.NoError(t, err)
		err = tc.Submit(tj)
		require.NoError(t, err)
		results, err := tc.Shutdown()
		require.NoError(t, err)
		assert.Equal(t, 1, len(results))

		// Check if object is cached.
		age, size, err = tc.CacheInfo(fed.Ctx, innerFileUrl)
		require.NoError(t, err)
		assert.Equal(t, int64(len(testFileContent)), size)
		require.NotEqual(t, -1, age)
	}
}

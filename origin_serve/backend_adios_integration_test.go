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

package origin_serve

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/webdav"

	"github.com/pelicanplatform/pelican/server_structs"
	"github.com/pelicanplatform/pelican/server_utils"
)

// adiosUpstreamRecorder is a mock ADIOS service that records the exact
// escaped path and method of every request (availability probes against
// "/" excluded).
type adiosUpstreamRecorder struct {
	mu       sync.Mutex
	requests []string // "METHOD escaped-path"
	payload  []byte
}

func (rec *adiosUpstreamRecorder) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			rec.mu.Lock()
			rec.requests = append(rec.requests, r.Method+" "+r.URL.EscapedPath())
			rec.mu.Unlock()
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(rec.payload)))
		w.WriteHeader(http.StatusOK)
		if r.Method != http.MethodHead {
			_, _ = w.Write(rec.payload)
		}
	}
}

func (rec *adiosUpstreamRecorder) recorded() []string {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return append([]string(nil), rec.requests...)
}

func (rec *adiosUpstreamRecorder) reset() {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	rec.requests = nil
}

// setupAdiosHandlerTest wires the real RegisterHandlers/handleRequest
// stack in front of an ADIOS passthrough backend, mirroring the
// pattern in TestPathTraversal_HandleRequest.
func setupAdiosHandlerTest(t *testing.T, upstreamURL string) *gin.Engine {
	gin.SetMode(gin.TestMode)

	backend := newAdiosBackend(AdiosBackendOptions{
		ServiceURL:    upstreamURL,
		StoragePrefix: "/adios",
	})

	ResetHandlers()
	t.Cleanup(func() {
		ResetHandlers()
		globalAuthConfig = nil
	})

	backends = map[string]server_utils.OriginBackend{
		"/fdp-itb/adios": backend,
	}
	webdavHandlers = map[string]*webdav.Handler{
		"/fdp-itb/adios": {
			FileSystem: backend.FileSystem(),
			LockSystem: webdav.NewMemLS(),
		},
	}
	exportPrefixMap = map[string]string{
		"/fdp-itb/adios": "/adios",
	}

	exports := []server_utils.OriginExport{{
		FederationPrefix: "/fdp-itb/adios",
		StoragePrefix:    "/adios",
		Capabilities:     server_structs.Capabilities{PublicReads: true},
	}}
	ac := &authConfig{}
	ac.exports.Store(&exports)
	globalAuthConfig = ac

	engine := gin.New()
	require.NoError(t, RegisterHandlers(engine, false))
	return engine
}

// TestAdiosHandlerEndToEnd sends a GET through the full gin + WebDAV +
// backend stack and verifies the upstream ADIOS service receives the
// path byte-for-byte — percent-encoded separators included — with the
// FederationPrefix stripped and the StoragePrefix prepended.
func TestAdiosHandlerEndToEnd(t *testing.T) {
	rec := &adiosUpstreamRecorder{payload: []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}}
	upstream := httptest.NewServer(rec.handler())
	defer upstream.Close()

	engine := setupAdiosHandlerTest(t, upstream.URL)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/fdp-itb/adios/cfs/www/KSTAR/images.bp/bbb%2Fphi/g~L2pzYXRy~c26o0", nil)
	engine.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	assert.Equal(t, rec.payload, w.Body.Bytes())
	assert.Equal(t, fmt.Sprintf("%d", len(rec.payload)), w.Header().Get("Content-Length"))

	// Exactly one upstream GET, path verbatim: %2F not collapsed, "~"
	// untouched, no query string synthesized.
	assert.Equal(t, []string{"GET /adios/cfs/www/KSTAR/images.bp/bbb%2Fphi/g~L2pzYXRy~c26o0"}, rec.recorded())
}

// TestAdiosHandlerHead verifies a client HEAD is served from a single
// upstream HEAD — no upstream GET whose body would be thrown away —
// and reports the upstream's Content-Length.
func TestAdiosHandlerHead(t *testing.T) {
	rec := &adiosUpstreamRecorder{payload: []byte("head-payload")}
	upstream := httptest.NewServer(rec.handler())
	defer upstream.Close()

	engine := setupAdiosHandlerTest(t, upstream.URL)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodHead, "/fdp-itb/adios/f.bp/rads/s0n1b0r1", nil)
	engine.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, fmt.Sprintf("%d", len(rec.payload)), w.Header().Get("Content-Length"))
	assert.Equal(t, []string{"HEAD /adios/f.bp/rads/s0n1b0r1"}, rec.recorded())
}

// TestAdiosHandlerEncodedTraversal verifies that encoded traversal
// sequences (%2e%2e) are rejected before any request reaches the
// upstream service.
func TestAdiosHandlerEncodedTraversal(t *testing.T) {
	rec := &adiosUpstreamRecorder{payload: []byte("x")}
	upstream := httptest.NewServer(rec.handler())
	defer upstream.Close()

	engine := setupAdiosHandlerTest(t, upstream.URL)
	rec.reset() // drop anything from registration-time probes

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/fdp-itb/adios/f.bp/%2e%2e/rads", nil)
	engine.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Empty(t, rec.recorded(), "upstream must not be contacted for traversal paths")
}

// TestAdiosHandlerRepeatedGetStableETag verifies that two GETs for the
// same object return the same ETag, so downstream caches can
// revalidate.  (A time.Now()-derived mtime would change per request.)
func TestAdiosHandlerRepeatedGetStableETag(t *testing.T) {
	rec := &adiosUpstreamRecorder{payload: []byte("etag-payload")}
	upstream := httptest.NewServer(rec.handler())
	defer upstream.Close()

	engine := setupAdiosHandlerTest(t, upstream.URL)

	getETag := func() string {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/fdp-itb/adios/f.bp/rads/s0n1b0r1", nil)
		engine.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)
		return w.Header().Get("ETag")
	}

	first := getETag()
	second := getETag()
	require.NotEmpty(t, first)
	assert.Equal(t, first, second, "ETag must be stable across requests")
}

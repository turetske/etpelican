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
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pelicanplatform/pelican/server_utils"
)

func TestAdiosUpstreamURL(t *testing.T) {
	fs := &adiosFileSystem{
		serviceURL:    "https://example.org",
		storagePrefix: "/adios",
	}

	// Paths are forwarded verbatim: no query synthesis, no reordering,
	// no decoding.
	u, err := fs.upstreamURL("/cfs/www/KSTAR/images.bp/r1/g~L2pzYXRy~c26o0")
	require.NoError(t, err)
	assert.Equal(t, "https://example.org/adios/cfs/www/KSTAR/images.bp/r1/g~L2pzYXRy~c26o0", u)

	// Percent-encoded separators must survive untouched — decoding
	// %2F would change which variable is requested.
	u, err = fs.upstreamURL("/savedt.bp/bbb%2Fphi/s0n1b0r1")
	require.NoError(t, err)
	assert.Equal(t, "https://example.org/adios/savedt.bp/bbb%2Fphi/s0n1b0r1", u)

	// Characters common in base64-ish ADIOS request segments.
	u, err = fs.upstreamURL("/f.bp/rads+te+jsatr/aGk9PQ==")
	require.NoError(t, err)
	assert.Equal(t, "https://example.org/adios/f.bp/rads+te+jsatr/aGk9PQ==", u)

	// Empty and "." segments are dropped.
	u, err = fs.upstreamURL("//a/./b")
	require.NoError(t, err)
	assert.Equal(t, "https://example.org/adios/a/b", u)

	// Traversal is rejected, in both literal and encoded forms.
	_, err = fs.upstreamURL("/a/../b")
	require.Error(t, err)
	_, err = fs.upstreamURL("/a/%2e%2e/b")
	require.Error(t, err)
	_, err = fs.upstreamURL("/a/%2E%2E/b")
	require.Error(t, err)

	// Empty paths are rejected.
	_, err = fs.upstreamURL("")
	require.Error(t, err)
	_, err = fs.upstreamURL("/")
	require.Error(t, err)

	// Invalid percent-encoding is rejected rather than forwarded.
	_, err = fs.upstreamURL("/a/b%zz")
	require.Error(t, err)
}

func TestAdiosUpstreamURLNoPrefix(t *testing.T) {
	fs := &adiosFileSystem{serviceURL: "https://example.org"}
	u, err := fs.upstreamURL("/f.bp/v/s0n1b0r1")
	require.NoError(t, err)
	assert.Equal(t, "https://example.org/f.bp/v/s0n1b0r1", u)
}

func TestAdiosEscapedPath(t *testing.T) {
	// The raw path stashed by the handler wins over the decoded name.
	ctx := server_utils.WithRawRequest(context.Background(), &server_utils.RawRequest{
		EscapedPath: "/f.bp/bbb%2Fphi/s0n1b0r1",
		Method:      http.MethodGet,
	})
	assert.Equal(t, "/f.bp/bbb%2Fphi/s0n1b0r1", adiosEscapedPath(ctx, "/f.bp/bbb/phi/s0n1b0r1"))

	// Without a stashed raw path, the decoded name is re-escaped.
	assert.Equal(t, "/f.bp/with%20space", adiosEscapedPath(context.Background(), "/f.bp/with space"))
}

func TestAdiosStatUpstream(t *testing.T) {
	var status atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodHead, r.Method)
		code := int(status.Load())
		if code == http.StatusOK {
			w.Header().Set("Content-Length", "1234")
		}
		w.WriteHeader(code)
	}))
	defer srv.Close()

	fs := &adiosFileSystem{
		serviceURL: srv.URL,
		httpClient: srv.Client(),
	}

	status.Store(http.StatusOK)
	size, _, err := fs.statUpstream(context.Background(), srv.URL+"/f.bp/v/s0n1b0r1")
	require.NoError(t, err)
	assert.Equal(t, int64(1234), size)

	status.Store(http.StatusNotFound)
	_, _, err = fs.statUpstream(context.Background(), srv.URL+"/f.bp/v/s0n1b0r1")
	assert.ErrorIs(t, err, os.ErrNotExist)

	// Servers predating ADIOS PR 5144 refuse HEAD; degrade to size 0
	// rather than failing.
	status.Store(http.StatusForbidden)
	size, _, err = fs.statUpstream(context.Background(), srv.URL+"/f.bp/v/s0n1b0r1")
	require.NoError(t, err)
	assert.Equal(t, int64(0), size)

	status.Store(http.StatusInternalServerError)
	_, _, err = fs.statUpstream(context.Background(), srv.URL+"/f.bp/v/s0n1b0r1")
	require.Error(t, err)
}

func TestAdiosOpenFileGet(t *testing.T) {
	var gets, heads atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			gets.Add(1)
		case http.MethodHead:
			heads.Add(1)
		}
		// The upstream must see the path byte-for-byte, encoding intact.
		assert.Equal(t, "/adios/f.bp/bbb%2Fphi/s0n1b0r1", r.URL.EscapedPath())
		assert.Empty(t, r.URL.RawQuery)
		_, _ = w.Write([]byte("payload"))
	}))
	defer srv.Close()

	backend := newAdiosBackend(AdiosBackendOptions{
		ServiceURL:    srv.URL,
		StoragePrefix: "/adios",
	})

	ctx := server_utils.WithRawRequest(context.Background(), &server_utils.RawRequest{
		EscapedPath: "/f.bp/bbb%2Fphi/s0n1b0r1",
		Method:      http.MethodGet,
	})
	f, err := backend.fs.OpenFile(ctx, "/f.bp/bbb/phi/s0n1b0r1", os.O_RDONLY, 0)
	require.NoError(t, err)
	defer f.Close()

	// A GET-serving open sizes the object from the eager response — no
	// upstream HEAD.
	fi, err := f.Stat()
	require.NoError(t, err)
	assert.Equal(t, int64(len("payload")), fi.Size())

	data, err := io.ReadAll(f)
	require.NoError(t, err)
	assert.Equal(t, "payload", string(data))

	assert.Equal(t, int64(1), gets.Load())
	assert.Equal(t, int64(0), heads.Load())
}

func TestAdiosOpenFileHead(t *testing.T) {
	var gets, heads atomic.Int64
	payload := []byte("head-then-get")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(payload)))
		switch r.Method {
		case http.MethodHead:
			heads.Add(1)
		case http.MethodGet:
			gets.Add(1)
			_, _ = w.Write(payload)
		}
	}))
	defer srv.Close()

	backend := newAdiosBackend(AdiosBackendOptions{
		ServiceURL:    srv.URL,
		StoragePrefix: "/adios",
	})

	ctx := server_utils.WithRawRequest(context.Background(), &server_utils.RawRequest{
		EscapedPath: "/f.bp/v/s0n1b0r1",
		Method:      http.MethodHead,
	})
	f, err := backend.fs.OpenFile(ctx, "/f.bp/v/s0n1b0r1", os.O_RDONLY, 0)
	require.NoError(t, err)
	defer f.Close()

	// A HEAD-serving open must not trigger an upstream GET: sizing comes
	// from HEAD, and ServeContent's End/Start seeks are arithmetic.
	fi, err := f.Stat()
	require.NoError(t, err)
	assert.Equal(t, int64(len(payload)), fi.Size())

	end, err := f.Seek(0, io.SeekEnd)
	require.NoError(t, err)
	assert.Equal(t, int64(len(payload)), end)
	_, err = f.Seek(0, io.SeekStart)
	require.NoError(t, err)

	assert.Equal(t, int64(1), heads.Load())
	assert.Equal(t, int64(0), gets.Load())

	// Reading lazily issues the GET.
	data, err := io.ReadAll(f)
	require.NoError(t, err)
	assert.Equal(t, payload, data)
	assert.Equal(t, int64(1), gets.Load())
}

func TestAdiosStreamSeek(t *testing.T) {
	var gets atomic.Int64
	payload := []byte("0123456789")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			gets.Add(1)
		}
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	backend := newAdiosBackend(AdiosBackendOptions{
		ServiceURL:    srv.URL,
		StoragePrefix: "/adios",
	})

	f, err := backend.fs.OpenFile(context.Background(), "/f.bp/v/s0n1b0r1", os.O_RDONLY, 0)
	require.NoError(t, err)
	defer f.Close()

	// The http.ServeContent pattern: Seek(End) to size, Seek(Start),
	// then sequential reads — served by the single eager GET.
	end, err := f.Seek(0, io.SeekEnd)
	require.NoError(t, err)
	assert.Equal(t, int64(len(payload)), end)
	_, err = f.Seek(0, io.SeekStart)
	require.NoError(t, err)

	data, err := io.ReadAll(f)
	require.NoError(t, err)
	assert.Equal(t, payload, data)
	assert.Equal(t, int64(1), gets.Load())

	// A backward seek forces a re-fetch that discards up to the offset.
	_, err = f.Seek(4, io.SeekStart)
	require.NoError(t, err)
	data, err = io.ReadAll(f)
	require.NoError(t, err)
	assert.Equal(t, payload[4:], data)
	assert.Equal(t, int64(2), gets.Load())
}

func TestAdiosOpenFileNoContentLength(t *testing.T) {
	payload := []byte("chunked-payload")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Force a chunked response so ContentLength is unknown.
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	backend := newAdiosBackend(AdiosBackendOptions{
		ServiceURL:    srv.URL,
		StoragePrefix: "/adios",
	})

	f, err := backend.fs.OpenFile(context.Background(), "/f.bp/v/s0n1b0r1", os.O_RDONLY, 0)
	require.NoError(t, err)
	defer f.Close()

	// Without a Content-Length the backend buffers to learn the size.
	fi, err := f.Stat()
	require.NoError(t, err)
	assert.Equal(t, int64(len(payload)), fi.Size())

	data, err := io.ReadAll(f)
	require.NoError(t, err)
	assert.Equal(t, payload, data)
}

func TestAdiosCheckAvailabilityMemoized(t *testing.T) {
	var probes atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodHead, r.Method)
		probes.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	backend := newAdiosBackend(AdiosBackendOptions{ServiceURL: srv.URL})

	require.NoError(t, backend.CheckAvailability())
	require.NoError(t, backend.CheckAvailability())
	require.NoError(t, backend.CheckAvailability())
	assert.Equal(t, int64(1), probes.Load(), "availability probes should be memoized")
}

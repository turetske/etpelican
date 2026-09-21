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
	"strconv"
	"sync"
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

func TestAdiosUpstreamURLAdminRoot(t *testing.T) {
	fs := &adiosFileSystem{serviceURL: "https://example.org"}

	// A leading "_adios" segment addresses the ADIOS server's admin
	// commands (stats, files, flush, limits), which are unauthenticated
	// upstream.  It must never be forwarded, in any spelling.
	for _, p := range []string{
		"/_adios/flush",
		"/_adios/stats",
		"/_adios",
		"//_adios/limits", // empty leading segment is dropped first
		"/./_adios/flush", // "." segment is dropped first
		"/%5Fadios/flush", // percent-encoded underscore
	} {
		_, err := fs.upstreamURL(p)
		require.Error(t, err, p)
		assert.Contains(t, err.Error(), "admin", p)
	}

	// An encoded slash makes "_adios/flush" a single segment — a dataset
	// name as far as the server is concerned, not the admin root.  It is
	// forwarded verbatim (the server 404s it), not blocked.
	u, err := fs.upstreamURL("/_adios%2Fflush")
	require.NoError(t, err)
	assert.Equal(t, "https://example.org/_adios%2Fflush", u)

	// The marker is legitimate anywhere *after* a dataset path — that is
	// how every current client addresses data — including when the
	// dataset itself is a single segment.
	for _, p := range []string{
		"/cfs/www/KSTAR24.tar/_adios/v1r1pAAAA/b~2~L2JiYi9uZw~c66,26,1o0,0,0~L2JiYi90ZQ~c66,26o0,0",
		"/images.bp/_adios/v1r1/g~L2pzYXRy~c26o0",
		"/f.bp/_adios/stats", // looks like a command but is a data request for f.bp
	} {
		u, err := fs.upstreamURL(p)
		require.NoError(t, err, p)
		assert.Equal(t, "https://example.org"+p, u)
	}

	// With a StoragePrefix the forwarded path can never start with
	// "_adios" anyway, but the client-facing rule is the same.
	fs.storagePrefix = "/adios"
	_, err = fs.upstreamURL("/_adios/flush")
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

// rangeServer serves payload, honoring `Range: bytes=N-` with a 206, and
// records every Range header it receives ("" when absent) plus the
// number of body bytes it actually sent per request.
type rangeServer struct {
	mu      sync.Mutex
	payload []byte
	ranges  []string
	methods []string
	sent    []int
}

func (rs *rangeServer) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rs.mu.Lock()
		rs.methods = append(rs.methods, r.Method)
		rs.ranges = append(rs.ranges, r.Header.Get("Range"))
		rs.mu.Unlock()

		start := 0
		status := http.StatusOK
		if h := r.Header.Get("Range"); h != "" {
			fmt.Sscanf(h, "bytes=%d-", &start)
			status = http.StatusPartialContent
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(rs.payload)-1, len(rs.payload)))
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(rs.payload)-start))
		w.WriteHeader(status)
		n := 0
		if r.Method != http.MethodHead {
			n, _ = w.Write(rs.payload[start:])
		}
		rs.mu.Lock()
		rs.sent = append(rs.sent, n)
		rs.mu.Unlock()
	}
}

// TestAdiosStreamRangeHonored verifies that a re-positioned read asks
// upstream for `Range: bytes=<offset>-` and serves the 206 directly,
// rather than fetching from byte 0 and discarding.
func TestAdiosStreamRangeHonored(t *testing.T) {
	rs := &rangeServer{payload: []byte("0123456789")}
	srv := httptest.NewServer(rs.handler())
	defer srv.Close()

	backend := newAdiosBackend(AdiosBackendOptions{ServiceURL: srv.URL})
	f, err := backend.fs.OpenFile(context.Background(), "/f.bp/_adios/v1r1/g~x~c10o0", os.O_RDONLY, 0)
	require.NoError(t, err)
	defer f.Close()

	// Backward seek after the eager whole-object GET.
	_, err = f.Seek(4, io.SeekStart)
	require.NoError(t, err)
	data, err := io.ReadAll(f)
	require.NoError(t, err)
	assert.Equal(t, []byte("456789"), data)

	assert.Equal(t, []string{"", "bytes=4-"}, rs.ranges, "second GET must carry the offset as a Range")
	assert.Equal(t, []int{10, 6}, rs.sent, "upstream must send only the tail on the ranged GET")

	// Size stays that of the whole object, not of the partial body.
	fi, err := f.Stat()
	require.NoError(t, err)
	assert.Equal(t, int64(10), fi.Size())
}

// TestAdiosOpenFileRangedGet verifies that a client GET carrying a
// Range header does not open an eager upstream GET from byte 0: the
// object is sized with HEAD and the single GET goes out at the
// requested offset.  This is the 55 GB tar-member case from ADIOS PR
// 5176 — an abandoned GET from 0 there is a real download upstream.
func TestAdiosOpenFileRangedGet(t *testing.T) {
	rs := &rangeServer{payload: []byte("0123456789")}
	srv := httptest.NewServer(rs.handler())
	defer srv.Close()

	backend := newAdiosBackend(AdiosBackendOptions{ServiceURL: srv.URL})
	ctx := server_utils.WithRawRequest(context.Background(), &server_utils.RawRequest{
		EscapedPath: "/cfs/www/KSTAR24.tar",
		Method:      http.MethodGet,
		Range:       "bytes=4-7",
	})
	f, err := backend.fs.OpenFile(ctx, "/cfs/www/KSTAR24.tar", os.O_RDONLY, 0)
	require.NoError(t, err)
	defer f.Close()

	assert.Equal(t, []string{http.MethodHead}, rs.methods, "open must size with HEAD only")

	// http.ServeContent's pattern for a range: seek to the start, read
	// the requested length.
	_, err = f.Seek(4, io.SeekStart)
	require.NoError(t, err)
	buf := make([]byte, 4)
	_, err = io.ReadFull(f, buf)
	require.NoError(t, err)
	assert.Equal(t, []byte("4567"), buf)

	assert.Equal(t, []string{http.MethodHead, http.MethodGet}, rs.methods)
	assert.Equal(t, []string{"", "bytes=4-"}, rs.ranges, "the only GET starts at the range offset")
	assert.Equal(t, []int{0, 6}, rs.sent, "nothing is fetched from byte 0")
}

func TestParseContentRange(t *testing.T) {
	first, total, ok := parseContentRange("bytes 4-9/10")
	require.True(t, ok)
	assert.Equal(t, int64(4), first)
	assert.Equal(t, int64(10), total)

	first, total, ok = parseContentRange("bytes 100-199/*")
	require.True(t, ok)
	assert.Equal(t, int64(100), first)
	assert.Equal(t, int64(-1), total)

	for _, bad := range []string{"", "bytes */10", "items 0-1/2", "bytes 0-1"} {
		_, _, ok = parseContentRange(bad)
		assert.False(t, ok, bad)
	}
}

// TestAdiosStreamSeek covers the fallback: an upstream that ignores the
// Range header and answers 200 from byte 0 still yields the right bytes
// (the leading offset is discarded).
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

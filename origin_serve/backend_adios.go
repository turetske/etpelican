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

// The ADIOS backend is a straight passthrough to an upstream ADIOS
// service.  The origin does not understand the ADIOS request grammar:
// it strips the export's FederationPrefix (done by the handler layer),
// prepends the export's StoragePrefix (the ADIOS route prefix, e.g.
// "/adios"), and forwards the remainder of the path verbatim —
// percent-encoding included.  ADIOS encodes everything it needs
// (variable names, step/block selectors, file configs) in the path, so
// any rewriting here would silently change request semantics.
package origin_serve

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/webdav"

	log "github.com/sirupsen/logrus"

	"github.com/pelicanplatform/pelican/config"
	"github.com/pelicanplatform/pelican/server_utils"
)

const (
	// How long a successful availability probe of the upstream ADIOS
	// service is trusted before re-probing.
	adiosAvailabilityOkTTL = 30 * time.Second
	// How long a failed probe is trusted; kept short so a recovered
	// upstream is noticed quickly.
	adiosAvailabilityFailTTL = 5 * time.Second
)

type adiosBackend struct {
	fs *adiosFileSystem

	// Availability probes are memoized so that per-request
	// CheckAvailability calls don't each hit the upstream service.
	availMu    sync.Mutex
	availUntil time.Time
	availErr   error
}

type AdiosBackendOptions struct {
	ServiceURL    string
	StoragePrefix string
	AuthTokenFile string
}

func newAdiosBackend(opts AdiosBackendOptions) *adiosBackend {
	fs := &adiosFileSystem{
		serviceURL:    strings.TrimSuffix(opts.ServiceURL, "/"),
		storagePrefix: opts.StoragePrefix,
		authTokenFile: opts.AuthTokenFile,
		httpClient:    &http.Client{Transport: config.GetTransport()},
	}
	return &adiosBackend{fs: fs}
}

func (b *adiosBackend) CheckAvailability() error {
	b.availMu.Lock()
	defer b.availMu.Unlock()
	if time.Now().Before(b.availUntil) {
		return b.availErr
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := b.fs.newUpstreamRequest(ctx, http.MethodHead, b.fs.serviceURL)
	if err != nil {
		return err
	}

	ttl := adiosAvailabilityFailTTL
	resp, err := b.fs.httpClient.Do(req)
	if err != nil {
		b.availErr = err
	} else {
		resp.Body.Close()
		if resp.StatusCode >= 500 {
			b.availErr = fmt.Errorf("adios backend probe failed with status %d", resp.StatusCode)
		} else {
			b.availErr = nil
			ttl = adiosAvailabilityOkTTL
		}
	}
	b.availUntil = time.Now().Add(ttl)
	return b.availErr
}

func (b *adiosBackend) FileSystem() webdav.FileSystem { return b.fs }
func (b *adiosBackend) Checksummer() server_utils.OriginChecksummer {
	return nil
}

// DefaultContentType marks ADIOS payloads as opaque binary.  Setting it
// up front keeps http.ServeContent from sniffing the type by reading
// the body — an upstream GET the passthrough would otherwise issue even
// for HEAD requests, since ADIOS object names carry no file extension.
func (b *adiosBackend) DefaultContentType() string {
	return "application/octet-stream"
}

type adiosFileSystem struct {
	serviceURL    string
	storagePrefix string
	authTokenFile string
	httpClient    *http.Client

	// Warn only once when the upstream server predates HEAD support
	// (ADIOS PR 5144) and we have to degrade Stat to a zero size.
	headFallbackWarn sync.Once
}

func (fs *adiosFileSystem) Mkdir(context.Context, string, os.FileMode) error {
	return os.ErrPermission
}

func (fs *adiosFileSystem) OpenFile(ctx context.Context, name string, flag int, _ os.FileMode) (webdav.File, error) {
	if flag != os.O_RDONLY {
		return nil, os.ErrPermission
	}

	upstream, err := fs.upstreamURL(adiosEscapedPath(ctx, name))
	if err != nil {
		log.Debugf("Rejecting ADIOS path %q: %v", name, err)
		return nil, os.ErrNotExist
	}
	log.Debugf("ADIOS upstream URL: %s", upstream)

	f := &adiosStreamFile{
		fs:   fs,
		ctx:  ctx,
		name: name,
		url:  upstream,
		mod:  time.Unix(0, 0),
	}

	// For a GET, fetch eagerly so the data arrives in a single upstream
	// round trip and the response's Content-Length sizes the object.
	// For anything else (HEAD in particular — the WebDAV layer serves
	// both through the same code path), size the object with an
	// upstream HEAD and only issue the GET if a byte is actually read;
	// ADIOS requests are computed server-side, so a discarded GET body
	// is real wasted work upstream.
	method := http.MethodGet
	if rr := server_utils.RawRequestFromContext(ctx); rr != nil && rr.Method != "" {
		method = rr.Method
	}
	if method == http.MethodGet {
		if err := f.fetch(0); err != nil {
			return nil, err
		}
	} else {
		size, mod, err := fs.statUpstream(ctx, upstream)
		if err != nil {
			return nil, err
		}
		f.size, f.sized, f.mod = size, true, mod
	}
	return f, nil
}

func (fs *adiosFileSystem) RemoveAll(context.Context, string) error {
	return os.ErrPermission
}

func (fs *adiosFileSystem) Rename(context.Context, string, string) error {
	return os.ErrPermission
}

func (fs *adiosFileSystem) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	upstream, err := fs.upstreamURL(adiosEscapedPath(ctx, name))
	if err != nil {
		return nil, os.ErrNotExist
	}
	size, mod, err := fs.statUpstream(ctx, upstream)
	if err != nil {
		return nil, err
	}
	return &adiosFileInfo{name: path.Base(name), size: size, mod: mod}, nil
}

// statUpstream sizes an object with an upstream HEAD request.  Servers
// predating ADIOS PR 5144 refuse HEAD (403/405); degrade to a zero size
// there instead of failing the request outright.
func (fs *adiosFileSystem) statUpstream(ctx context.Context, upstream string) (int64, time.Time, error) {
	req, err := fs.newUpstreamRequest(ctx, http.MethodHead, upstream)
	if err != nil {
		return 0, time.Time{}, err
	}
	resp, err := fs.httpClient.Do(req)
	if err != nil {
		return 0, time.Time{}, err
	}
	defer resp.Body.Close()

	mod := time.Unix(0, 0)
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return 0, time.Time{}, os.ErrNotExist
	case resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusMethodNotAllowed || resp.StatusCode == http.StatusNotImplemented:
		fs.headFallbackWarn.Do(func() {
			log.Warnf("Upstream ADIOS service refused a HEAD request with status %d; "+
				"object sizes will be reported as 0 until the server supports HEAD (ADIOS PR 5144)", resp.StatusCode)
		})
		return 0, mod, nil
	case resp.StatusCode/100 == 2:
		size := resp.ContentLength
		if size < 0 {
			size = 0
		}
		if lm, err := http.ParseTime(resp.Header.Get("Last-Modified")); err == nil {
			mod = lm
		}
		return size, mod, nil
	default:
		return 0, time.Time{}, fmt.Errorf("adios HEAD request failed with status %d", resp.StatusCode)
	}
}

// upstreamURL builds the backend URL for the given escaped request
// path: service URL + storage prefix + the path verbatim.  The only
// rewriting performed is safety validation — traversal segments are
// rejected and empty/"." segments dropped.
func (fs *adiosFileSystem) upstreamURL(escapedPath string) (string, error) {
	segments := make([]string, 0, strings.Count(escapedPath, "/")+1)
	for _, seg := range strings.Split(escapedPath, "/") {
		if seg == "" || seg == "." {
			continue
		}
		if seg == ".." {
			return "", fmt.Errorf("path traversal in adios path %q", escapedPath)
		}
		// Catch encoded traversal (%2e%2e) that the upstream server or
		// an intermediary might decode and normalize.
		if decoded, err := url.PathUnescape(seg); err != nil {
			return "", fmt.Errorf("invalid percent-encoding in adios path segment %q: %w", seg, err)
		} else if decoded == ".." || decoded == "." {
			return "", fmt.Errorf("path traversal in adios path %q", escapedPath)
		}
		segments = append(segments, seg)
	}
	if len(segments) == 0 {
		return "", fmt.Errorf("empty adios path")
	}

	base := fs.serviceURL
	if prefix := strings.Trim(fs.storagePrefix, "/"); prefix != "" {
		base += "/" + prefix
	}
	return base + "/" + strings.Join(segments, "/"), nil
}

// newUpstreamRequest builds a request to the ADIOS service, attaching
// the bearer token (if configured) and any stashed Pelican tracing
// headers.
func (fs *adiosFileSystem) newUpstreamRequest(ctx context.Context, method, upstream string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, upstream, nil)
	if err != nil {
		return nil, err
	}
	if token := fs.readAuthToken(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if ph := server_utils.PelicanHeadersFromContext(ctx); ph != nil {
		if ph.JobId != "" {
			req.Header.Set("X-Pelican-JobId", ph.JobId)
		}
		if ph.Timeout != "" {
			req.Header.Set("X-Pelican-Timeout", ph.Timeout)
		}
	}
	return req, nil
}

func (fs *adiosFileSystem) readAuthToken() string {
	if fs.authTokenFile == "" {
		return ""
	}
	data, err := os.ReadFile(fs.authTokenFile)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// adiosEscapedPath returns the percent-encoded path to forward
// upstream, preferring the raw path stashed by the origin handler (the
// WebDAV layer only sees the decoded form, which collapses encoded
// separators like %2F).  Falls back to re-escaping the decoded name for
// callers outside the HTTP request path (e.g. tests).
func adiosEscapedPath(ctx context.Context, name string) string {
	if rr := server_utils.RawRequestFromContext(ctx); rr != nil && rr.EscapedPath != "" {
		return rr.EscapedPath
	}
	return (&url.URL{Path: name}).EscapedPath()
}

type adiosFileInfo struct {
	name  string
	size  int64
	mod   time.Time
	isDir bool
}

func (fi *adiosFileInfo) Name() string      { return fi.name }
func (fi *adiosFileInfo) Size() int64       { return fi.size }
func (fi *adiosFileInfo) Mode() os.FileMode { return 0444 }

// ModTime must be stable across requests: the WebDAV layer derives
// ETags from mtime+size, and a changing mtime (e.g. time.Now()) makes
// every response look modified to downstream caches.
func (fi *adiosFileInfo) ModTime() time.Time {
	if fi.mod.IsZero() {
		return time.Unix(0, 0)
	}
	return fi.mod
}
func (fi *adiosFileInfo) IsDir() bool      { return fi.isDir }
func (fi *adiosFileInfo) Sys() interface{} { return nil }

// adiosStreamFile is a read-only webdav.File that streams the upstream
// response instead of buffering it.  Seeks are arithmetic on a logical
// position (http.ServeContent seeks End→Start to size the content
// before reading); the underlying stream is reconciled lazily on Read —
// by discarding for forward gaps, or re-issuing the upstream request
// for backward ones.
type adiosStreamFile struct {
	fs   *adiosFileSystem
	ctx  context.Context
	name string
	url  string

	size  int64
	sized bool
	mod   time.Time

	pos     int64         // logical position (moved by Read/Seek)
	body    io.ReadCloser // current upstream stream, nil until fetched
	bodyPos int64         // position of the upstream stream
}

// fetch (re-)issues the upstream GET and positions the stream at
// offset.  On the first fetch the response sizes the object: from
// Content-Length when present, otherwise by buffering the whole body.
func (f *adiosStreamFile) fetch(offset int64) error {
	f.closeBody()

	req, err := f.fs.newUpstreamRequest(f.ctx, http.MethodGet, f.url)
	if err != nil {
		return err
	}
	resp, err := f.fs.httpClient.Do(req)
	if err != nil {
		return err
	}

	if resp.StatusCode == http.StatusNotFound {
		resp.Body.Close()
		return os.ErrNotExist
	}
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4*1024))
		resp.Body.Close()
		return fmt.Errorf("adios request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	body := resp.Body
	if !f.sized {
		if resp.ContentLength >= 0 {
			f.size = resp.ContentLength
		} else {
			// No Content-Length upstream; buffer to learn the size.
			payload, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				return err
			}
			f.size = int64(len(payload))
			body = io.NopCloser(bytes.NewReader(payload))
		}
		f.sized = true
		if lm, err := http.ParseTime(resp.Header.Get("Last-Modified")); err == nil {
			f.mod = lm
		}
	}

	if offset > 0 {
		if _, err := io.CopyN(io.Discard, body, offset); err != nil {
			body.Close()
			return fmt.Errorf("failed to skip to offset %d in adios response: %w", offset, err)
		}
	}
	f.body = body
	f.bodyPos = offset
	return nil
}

func (f *adiosStreamFile) closeBody() {
	if f.body != nil {
		f.body.Close()
		f.body = nil
	}
}

func (f *adiosStreamFile) Read(p []byte) (int, error) {
	if f.pos >= f.size {
		return 0, io.EOF
	}
	if f.body == nil || f.bodyPos != f.pos {
		if err := f.fetch(f.pos); err != nil {
			return 0, err
		}
	}
	n, err := f.body.Read(p)
	f.pos += int64(n)
	f.bodyPos += int64(n)
	return n, err
}

func (f *adiosStreamFile) Seek(offset int64, whence int) (int64, error) {
	var abs int64
	switch whence {
	case io.SeekStart:
		abs = offset
	case io.SeekCurrent:
		abs = f.pos + offset
	case io.SeekEnd:
		abs = f.size + offset
	default:
		return 0, fmt.Errorf("invalid seek whence %d", whence)
	}
	if abs < 0 {
		return 0, fmt.Errorf("negative seek position %d", abs)
	}
	f.pos = abs
	return abs, nil
}

func (f *adiosStreamFile) Close() error {
	f.closeBody()
	return nil
}

func (f *adiosStreamFile) Write(_ []byte) (int, error) {
	return 0, os.ErrPermission
}

func (f *adiosStreamFile) Readdir(_ int) ([]os.FileInfo, error) {
	return nil, fmt.Errorf("readdir not supported on file")
}

func (f *adiosStreamFile) Stat() (os.FileInfo, error) {
	return &adiosFileInfo{
		name: path.Base(f.name),
		size: f.size,
		mod:  f.mod,
	}, nil
}

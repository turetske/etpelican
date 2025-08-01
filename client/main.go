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

package client

import (
	"context"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"runtime/debug"
	"strings"
	"time"

	"math/rand"
	"os"
	"path"
	"path/filepath"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"github.com/pelicanplatform/pelican/config"
	"github.com/pelicanplatform/pelican/pelican_url"
	"github.com/pelicanplatform/pelican/server_structs"
)

// Number of caches to attempt to use in any invocation
var ObjectServersToTry int = 3

// Our own FileInfo structure to hold information about a file
// NOTE: this was created to provide more flexibility to information on a file. The fs.FileInfo interface was causing some issues like not always returning a Name attribute
// ALSO NOTE: the fields are exported so they can be marshalled into JSON, it does not work otherwise
type FileInfo struct {
	Name         string
	Size         int64
	ModTime      time.Time
	IsCollection bool
	Checksums    map[string]string `json:"checksums,omitempty"` // Checksum type (HTTP digest name) to hex-encoded value
}

// handleSchemelessIfNeeded is a helper function that updates the input discovery options to use a configured discovery
// URL for pelican URL parsing whenever the incoming path has no scheme. For example, a user command like:
//
//	pelican object get -f osg-htc.org /foo/bar
//
// results in a schemeless URL path of /foo/bar. This helper function will add a discovery option with the discovery
// URL so that pelican URL parsing doesn't fail.
func handleSchemelessIfNeeded(ctx context.Context, rpUrl *url.URL, dOpts *[]pelican_url.DiscoveryOption) error {
	// If the incoming URL has a scheme, there's nothing to do
	if rpUrl.Scheme != "" {
		return nil
	}

	fedInfo, err := config.GetFederation(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to get configured federation info")
	}

	// Placeholder for whatever discovery URL we end up using. We prefer the configured
	// discovery URL, but we can try to fallback to the Director if needed
	var discoveryUrl *url.URL
	if fedInfo.DiscoveryEndpoint != "" {
		if tmpUrl, err := url.Parse(fedInfo.DiscoveryEndpoint); err != nil {
			log.Warningf("Failed to parse configured discovery endpoint: %s", fedInfo.DiscoveryEndpoint)
		} else {
			discoveryUrl = tmpUrl
		}
	}

	if discoveryUrl == nil && fedInfo.DirectorEndpoint != "" {
		if tmpUrl, err := url.Parse(fedInfo.DirectorEndpoint); err != nil {
			log.Warningf("Failed to parse configured director endpoint: %s", fedInfo.DirectorEndpoint)
		} else {
			discoveryUrl = tmpUrl
		}
	}

	// If we still don't have a discovery URL, we can't proceed
	if discoveryUrl == nil {
		return errors.New("could not determine which discovery URL to use")
	}

	// If we have a discovery URL, add it to the discovery options
	*dOpts = append(*dOpts, pelican_url.WithDiscoveryUrl(discoveryUrl))
	return nil
}

// Given a remote path, use the client's wisdom to parse it as a Pelican URL, including metadata discovery.
//
// This will handle setting up the URL cache, passing along contexts to discovery, and passing the client context/user agent.
// Calling this should return a fully populated PelicanURL object, including any metadata that was discovered.
func ParseRemoteAsPUrl(ctx context.Context, rp string) (*pelican_url.PelicanURL, error) {
	rpUrl, err := url.Parse(rp)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse remote path")
	}

	// Set up options that get passed from Parse --> PopulateFedInfo and may be used when querying the Director
	client := config.GetClient()
	pOptions := []pelican_url.ParseOption{pelican_url.ShouldDiscover(true), pelican_url.ValidateQueryParams(true)}
	dOptions := []pelican_url.DiscoveryOption{pelican_url.UseCached(true), pelican_url.WithContext(ctx), pelican_url.WithClient(client), pelican_url.WithUserAgent(getUserAgent(""))}

	if err = handleSchemelessIfNeeded(ctx, rpUrl, &dOptions); err != nil {
		return nil, errors.Wrap(err, "failed to handle schemeless URL")
	}

	pUrl, err := pelican_url.Parse(
		rp,
		pOptions,
		dOptions,
	)
	if err != nil {
		return nil, err
	}

	return pUrl, nil
}

// Check the size of a remote file in an origin
func DoStat(ctx context.Context, destination string, options ...TransferOption) (fileInfo *FileInfo, err error) {

	defer func() {
		if r := recover(); r != nil {
			log.Debugln("Panic captured while attempting to stat:", r)
			log.Debugln("Panic caused by the following", string(debug.Stack()))
			ret := fmt.Sprintf("Unrecoverable error (panic) while check file size: %v", r)
			err = errors.New(ret)
			return
		}
	}()

	pUrl, err := ParseRemoteAsPUrl(ctx, destination)
	if err != nil {
		return
	}

	te, err := NewTransferEngine(ctx)
	if err != nil {
		return nil, err
	}

	defer func() {
		if err := te.Shutdown(); err != nil {
			log.Errorln("Failure when shutting down transfer engine:", err)
		}
	}()

	dirResp, err := GetDirectorInfoForPath(ctx, pUrl, http.MethodGet, "")
	if err != nil {
		return nil, err
	}

	var requestedChecksums []ChecksumType

	token := newTokenGenerator(pUrl, &dirResp, config.TokenSharedRead, true)
	for _, option := range options {
		switch option.Ident() {
		case identTransferOptionTokenLocation{}:
			token.SetTokenLocation(option.Value().(string))
		case identTransferOptionAcquireToken{}:
			token.EnableAcquire = option.Value().(bool)
		case identTransferOptionToken{}:
			token.SetToken(option.Value().(string))
		case identTransferOptionChecksums{}:
			requestedChecksums = option.Value().([]ChecksumType)
		}
	}

	var tokenContents string
	if dirResp.XPelNsHdr.RequireToken {
		tokenContents, err = token.get()
		if err != nil || tokenContents == "" {
			return nil, errors.Wrap(err, "failed to get token for transfer")
		}
	} else {
		token = nil
	}

	statInfo, err := statHttp(pUrl, dirResp, token)
	if err != nil {
		return nil, errors.Wrap(err, "failed to do the stat")
	}
	if len(requestedChecksums) > 0 {
		log.Debugln("Requested checksums:", requestedChecksums)
		project, _ := searchJobAd(attrProjectName)

		statUrls := make([]*url.URL, 0, len(dirResp.ObjectServers))
		if len(dirResp.ObjectServers) > 0 {
			for idx, oServer := range dirResp.ObjectServers {
				if idx > 2 {
					break
				}
				statUrls = append(statUrls, oServer)
			}
		} else {
			// If we don't have any object servers, we need to use the original URL
			statUrls = append(statUrls, pUrl.GetRawUrl())
		}

		var lastErr error
		for _, statUrl := range statUrls {
			checksums, err := fetchChecksum(ctx, requestedChecksums, statUrl, tokenContents, project)
			if err != nil {
				lastErr = err
				continue
			}
			statInfo.Checksums = make(map[string]string)
			for _, info := range checksums {
				statInfo.Checksums[HttpDigestFromChecksum(info.Algorithm)] = hex.EncodeToString(info.Value)
			}
		}
		if lastErr != nil {
			err = errors.Wrap(lastErr, "failed to fetch checksums")
			return
		}
	}
	return &statInfo, nil
}

// Check the cache information of a remote cache
func DoCacheInfo(ctx context.Context, destination string, options ...TransferOption) (age int, size int64, err error) {

	defer func() {
		if r := recover(); r != nil {
			log.Debugln("Panic captured while attempting to do cache info:", r)
			log.Debugln("Panic caused by the following", string(debug.Stack()))
			ret := fmt.Sprintf("Unrecoverable error (panic) while check file size: %v", r)
			err = errors.New(ret)
			return
		}
	}()

	pUrl, err := ParseRemoteAsPUrl(ctx, destination)
	if err != nil {
		return
	}

	te, err := NewTransferEngine(ctx)
	if err != nil {
		return
	}

	defer func() {
		if err := te.Shutdown(); err != nil {
			log.Errorln("Failure when shutting down transfer engine:", err)
		}
	}()

	tc, err := te.NewClient(options...)
	if err != nil {
		return
	}
	return tc.CacheInfo(ctx, pUrl.GetRawUrl())
}

func GetObjectServerHostnames(ctx context.Context, testFile string) (urls []string, err error) {
	pUrl, err := ParseRemoteAsPUrl(ctx, testFile)
	if err != nil {
		return
	}
	parsedDirResp, err := GetDirectorInfoForPath(ctx, pUrl, http.MethodGet, "")
	if err != nil {
		return
	}
	for _, objectServer := range parsedDirResp.ObjectServers {
		urls = append(urls, objectServer.Hostname())
	}

	return
}

func getUserAgent(project string) (agent string) {
	agent = "pelican-client/" + config.GetVersion()
	if project != "" {
		agent += " project/" + project
	}
	return
}

// Given a response from the director with sorted object servers, incorporate any "preferred" servers (origins/caches) that
// may be passed in from the command line. This should handle the special '+' logic -- if the user provides a list of servers
// and no +, it means they ONLY want to use the provided servers. Otherwise, we prefer those servers, but also incorporate the
// servers provided by the Director.
func generateSortedObjServers(dirResp server_structs.DirectorResponse, preferredCaches []*url.URL) (objectServers []*url.URL, err error) {
	var appendCaches bool
	// The global cache override is set
	if len(preferredCaches) > 0 {
		var preferredObjectServers []*url.URL
		for idx, preferredCache := range preferredCaches {
			cacheUrl := preferredCache.String()
			// If the preferred cache is empty, return an error
			if cacheUrl == "" {
				err = errors.New("Preferred server was specified as an empty string")
				return
			} else if cacheUrl == "+" {
				// If we have a '+' in our list, the user wants to prepend the preferred caches to the "normal" list of caches
				// if the cache is a '+', verify it is at the end of our list, if not, return an error
				if idx != len(preferredCaches)-1 {
					err = errors.New("The special character '+' must be the last item in the list of preferred servers")
					return
				}
				// We want to signify that we want to append the "normal" cache list
				appendCaches = true
			} else {
				// We have a normal item in the preferred cache list
				log.Debugf("Using the server (%s) from the config override\n", preferredCache)
				preferredObjectServers = append(preferredObjectServers, preferredCache)
			}
		}
		objectServers = preferredObjectServers
		// No +, no mo problems -- err, I mean, no more object servers
		if !appendCaches {
			return
		}
	}

	log.Debugln("Using the returned sources from the director")
	// We may have some servers from the preferred list
	objectServers = append(objectServers, dirResp.ObjectServers...)

	if log.IsLevelEnabled(log.DebugLevel) || log.IsLevelEnabled(log.TraceLevel) {
		oHosts := make([]string, len(objectServers))
		for idx, oServer := range objectServers {
			simpleUrl := url.URL{
				Scheme: oServer.Scheme,
				Host:   oServer.Host,
			}
			oHosts[idx] = simpleUrl.String()
		}
		if len(oHosts) <= 6 {
			log.Debugln("Matched object servers:", strings.Join(oHosts, ", "))
		} else {
			log.Debugf("Matched object servers: %s ... (plus %d more)", strings.Join(oHosts[0:6], ", "), len(oHosts)-6)
			log.Traceln("matched object servers continued:", oHosts[6:])
		}
	}
	return

}

// Function for the object ls command, we get target information for our remote object and eventually print out the contents of the specified object
func DoList(ctx context.Context, remoteObject string, options ...TransferOption) (fileInfos []FileInfo, err error) {
	// First, create a handler for any panics that occur
	defer func() {
		if r := recover(); r != nil {
			log.Debugln("Panic captured while attempting to perform transfer (DoList):", r)
			log.Debugln("Panic caused by the following", string(debug.Stack()))
			ret := fmt.Sprintf("Unrecoverable error (panic) captured in DoList: %v", r)
			err = errors.New(ret)
		}
	}()

	pUrl, err := ParseRemoteAsPUrl(ctx, remoteObject)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to parse remote path: %s", remoteObject)
	}

	te, err := NewTransferEngine(ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := te.Shutdown(); err != nil {
			log.Errorln("Failure when shutting down transfer engine:", err)
		}
	}()

	dirResp, err := GetDirectorInfoForPath(ctx, pUrl, http.MethodGet, "")
	if err != nil {
		return nil, err
	}

	// Get our token if needed
	token := newTokenGenerator(pUrl, &dirResp, config.TokenSharedRead, true)
	collectionsOverride := ""
	for _, option := range options {
		switch option.Ident() {
		case identTransferOptionTokenLocation{}:
			token.SetTokenLocation(option.Value().(string))
		case identTransferOptionAcquireToken{}:
			token.EnableAcquire = option.Value().(bool)
		case identTransferOptionToken{}:
			token.SetToken(option.Value().(string))
		case identTransferOptionCollectionsUrl{}:
			collectionsOverride = option.Value().(string)
		}
	}

	if dirResp.XPelNsHdr.RequireToken {
		tokenContents, err := token.get()
		if err != nil || tokenContents == "" {
			return nil, errors.Wrap(err, "failed to get token for transfer")
		}
	} else {
		token = nil
	}
	if collectionsOverride != "" {
		collectionsOverrideUrl, err := url.Parse(collectionsOverride)
		if err != nil {
			return nil, errors.Wrap(err, "unable to parse collections URL override")
		}
		dirResp.XPelNsHdr.CollectionsUrl = collectionsOverrideUrl
	}
	fileInfos, err = listHttp(pUrl, dirResp, token)
	if err != nil {
		return nil, errors.Wrap(err, "failed to perform list request")
	}

	return fileInfos, nil
}

// DoDelete queries the director using the DELETE HTTP method, retrieves the token, and initializes the delete operation.
func DoDelete(ctx context.Context, remoteDestination string, recursive bool, options ...TransferOption) (err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Debugln("Panic occurred while attempting to perform delete operation (DoDelete):", r)
			log.Debugln("Stack trace of the panic:", string(debug.Stack()))
			ret := fmt.Sprintf("Unrecoverable error (panic) in DoDelete: %v", r)
			err = errors.New(ret)
		}
	}()

	pUrl, err := ParseRemoteAsPUrl(ctx, remoteDestination)
	if err != nil {
		return errors.Wrapf(err, "failed to parse remote destination: %s", remoteDestination)
	}

	if _, exists := pUrl.Query()[pelican_url.QueryRecursive]; exists {
		recursive = true
	}

	dirResp, err := GetDirectorInfoForPath(ctx, pUrl, http.MethodDelete, "")
	if err != nil {
		return err
	}

	token := newTokenGenerator(pUrl, &dirResp, config.TokenDelete, true)
	for _, option := range options {
		switch option.Ident() {
		case identTransferOptionTokenLocation{}:
			token.SetTokenLocation(option.Value().(string))
		case identTransferOptionAcquireToken{}:
			token.EnableAcquire = option.Value().(bool)
		case identTransferOptionToken{}:
			token.SetToken(option.Value().(string))
		}
	}

	tokenContents, err := token.get()
	if err != nil || tokenContents == "" {
		return errors.Wrap(err, "failed to retrieve token for delete operation")
	}

	return deleteHttp(ctx, pUrl, recursive, dirResp, token)
}

/*
	Start of transfer for pelican object put, gets information from the target destination before doing our HTTP PUT request

localObject: the source file/directory you would like to upload
remoteDestination: the end location of the upload
recursive: a boolean indicating if the source is a directory or not
*/
func DoPut(ctx context.Context, localObject string, remoteDestination string, recursive bool, options ...TransferOption) (transferResults []TransferResults, err error) {
	// First, create a handler for any panics that occur
	defer func() {
		if r := recover(); r != nil {
			log.Debugln("Panic captured while attempting to perform transfer (DoPut):", r)
			log.Debugln("Panic caused by the following", string(debug.Stack()))
			ret := fmt.Sprintf("Unrecoverable error (panic) captured in DoPut: %v", r)
			err = errors.New(ret)
		}
	}()

	// Parse as a Pelican URL, but without any discovery (that happens when the transfer job is created).
	// We do this to handle URL validation early, and we allow unknown query params to be passed through so that old
	// clients may continue to function with newer directors/origins/caches. This will generate a warning about the query
	// but should still send it along.
	dOpts := []pelican_url.DiscoveryOption{}
	rpUrl, err := url.Parse(remoteDestination)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse remote destination while performing PUT")
	}
	dOpts = append(dOpts, pelican_url.WithContext(ctx))

	// If the incoming path has no scheme, we need to tell the pelican_url parser to use the configured discovery URL
	if err = handleSchemelessIfNeeded(ctx, rpUrl, &dOpts); err != nil {
		return nil, errors.Wrap(err, "failed to handle schemeless URL")
	}

	pUrl, err := pelican_url.Parse(remoteDestination, []pelican_url.ParseOption{pelican_url.ValidateQueryParams(true), pelican_url.AllowUnknownQueryParams(true)}, dOpts)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to parse remote object: %s", remoteDestination)
	}

	te, err := NewTransferEngine(ctx)
	if err != nil {
		return nil, err
	}

	defer func() {
		if err := te.Shutdown(); err != nil {
			log.Errorln("Failure when shutting down transfer engine:", err)
		}
	}()
	client, err := te.NewClient(options...)
	if err != nil {
		return
	}
	tj, err := client.NewTransferJob(context.Background(), pUrl.GetRawUrl(), localObject, true, recursive, options...)
	if err != nil {
		return
	}
	if err = client.Submit(tj); err != nil {
		return
	}
	transferResults, err = client.Shutdown()
	if tj.lookupErr != nil {
		err = tj.lookupErr
	}
	for _, result := range transferResults {
		if err == nil && result.Error != nil {
			err = result.Error
		}
	}
	return
}

/*
	Start of transfer for pelican object get, gets information from the target source before doing our HTTP GET request

remoteObject: the source file/directory you would like to upload
localDestination: the end location of the upload
recursive: a boolean indicating if the source is a directory or not
*/
func DoGet(ctx context.Context, remoteObject string, localDestination string, recursive bool, options ...TransferOption) (transferResults []TransferResults, err error) {
	// First, create a handler for any panics that occur
	defer func() {
		if r := recover(); r != nil {
			log.Debugln("Panic captured while attempting to perform transfer (DoGet):", r)
			log.Debugln("Panic caused by the following", string(debug.Stack()))
			ret := fmt.Sprintf("Unrecoverable error (panic) captured in DoGet: %v", r)
			err = errors.New(ret)
		}
	}()

	// Parse as a Pelican URL, but without any discovery (that happens when the transfer job is created).
	// We do this to handle URL validation early, and we allow unknown query params to be passed through so that old
	// clients may continue to function with newer directors/origins/caches. This will generate a warning about the query
	// but should still send it along.
	dOpts := []pelican_url.DiscoveryOption{}
	rpUrl, err := url.Parse(remoteObject)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse remote source while performing GET")
	}
	dOpts = append(dOpts, pelican_url.WithContext(ctx))

	// If the incoming path has no scheme, we need to tell the pelican_url parser to use the configured discovery URL
	if err = handleSchemelessIfNeeded(ctx, rpUrl, &dOpts); err != nil {
		return nil, errors.Wrap(err, "failed to handle schemeless URL")
	}

	pUrl, err := pelican_url.Parse(remoteObject, []pelican_url.ParseOption{pelican_url.ValidateQueryParams(true), pelican_url.AllowUnknownQueryParams(true)}, dOpts)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to parse remote object: %s", remoteObject)
	}

	// get absolute path
	localDestPath, _ := filepath.Abs(localDestination)

	//Check if path exists or if its in a folder
	if destStat, err := os.Stat(localDestPath); os.IsNotExist(err) {
		trailingChar := ""
		if string(localDestination[len(localDestination)-1]) == string(filepath.Separator) {
			trailingChar = string(filepath.Separator)
		}
		localDestination = localDestPath + trailingChar
	} else if destStat.IsDir() && pUrl.Query().Get(pelican_url.QueryPack) == "" {
		// If we have an auto-pack request, it's OK for the destination to be a directory
		// Otherwise, get the base name of the source and append it to the destination dir.
		// Note that we use the pUrl.Path, as this will have stripped any query params for us
		remoteObjectFilename := path.Base(pUrl.Path)
		if !recursive {
			localDestination = path.Join(localDestPath, remoteObjectFilename)
		}
	}

	success := false

	te, err := NewTransferEngine(ctx)
	if err != nil {
		return nil, err
	}

	defer func() {
		if err := te.Shutdown(); err != nil {
			log.Errorln("Failure when shutting down transfer engine:", err)
		}
	}()
	tc, err := te.NewClient(options...)
	if err != nil {
		return
	}
	tj, err := tc.NewTransferJob(context.Background(), pUrl.GetRawUrl(), localDestination, false, recursive)
	if err != nil {
		return
	}
	err = tc.Submit(tj)
	if err != nil {
		return
	}

	transferResults, err = tc.Shutdown()
	if err == nil {
		if tj.lookupErr == nil {
			success = true
		} else {
			err = tj.lookupErr
		}
	}
	var downloaded int64 = 0
	for _, result := range transferResults {
		downloaded += result.TransferredBytes
		if err == nil && result.Error != nil {
			success = false
			err = result.Error
		}
	}

	if success {
		// Get the final size of the download file
	} else {
		log.Errorln("Http GET failed! Unable to download file:", err)
	}

	if !success {
		// If there's only a single transfer error, remove the wrapping to provide
		// a simpler error message.  Results in:
		//    failed download from local-cache: server returned 404 Not Found
		// versus:
		//    failed to download file: transfer error: failed download from local-cache: server returned 404 Not Found
		var te *TransferErrors
		if errors.As(err, &te) {
			if len(te.Unwrap()) == 1 {
				var tae *TransferAttemptError
				if errors.As(te.Unwrap()[0], &tae) {
					return nil, tae
				} else {
					return nil, errors.Wrap(err, "failed to download file")
				}
			}
			return nil, te
		}
		return nil, errors.Wrap(err, "failed to download file")
	} else {
		return transferResults, err
	}
}

// Start the transfer, whether read or write back. Primarily used for backwards compatibility
func DoCopy(ctx context.Context, sourceFile string, destination string, recursive bool, options ...TransferOption) (transferResults []TransferResults, err error) {
	// First, create a handler for any panics that occur
	defer func() {
		if r := recover(); r != nil {
			log.Debugln("Panic captured while attempting to perform transfer (DoStashCPSingle):", r)
			log.Debugln("Panic caused by the following", string(debug.Stack()))
			ret := fmt.Sprintf("Unrecoverable error (panic) captured in DoStashCPSingle: %v", r)
			err = errors.New(ret)
		}
	}()

	var isPut bool
	// determine which direction we're headed
	parsedDest, err := url.Parse(destination)
	if err != nil {
		log.Errorln("Failed to parse destination URL:", err)
		return nil, err
	}
	parsedSrc, err := url.Parse(sourceFile)
	if err != nil {
		log.Errorln("Failed to parse source URL:", err)
		return nil, err
	}

	var localPath string
	var remotePath string
	if parsedDest.Scheme != "" && (parsedSrc.Scheme == "" || parsedSrc.Scheme == "file") {
		isPut = true
		log.Debugf("Detected a PUT from %s to %s", parsedSrc.Path, parsedDest.String())
		localPath = parsedSrc.Path
		remotePath = parsedDest.String()
	} else if (parsedDest.Scheme == "" || parsedDest.Scheme == "file") && parsedSrc.Scheme != "" {
		isPut = false
		log.Debugf("Detected a GET from %s to %s", parsedSrc.String(), parsedDest.Path)
		localPath = parsedDest.Path
		remotePath = parsedSrc.String()
	} else {
		return nil, errors.New("unable to determine direction of transfer.  Both source and destination are either local or remote")
	}

	if isPut {
		return DoPut(ctx, localPath, remotePath, recursive, options...)
	} else {
		return DoGet(ctx, remotePath, localPath, recursive, options...)
	}
}

// getIPs will resolve a hostname and return all corresponding IP addresses
// in DNS.  This can be used to randomly pick an IP when DNS round robin
// is used
func getIPs(name string) []string {
	var ipv4s []string
	var ipv6s []string

	info, err := net.LookupHost(name)
	if err != nil {
		log.Error("Unable to look up", name)

		var empty []string
		return empty
	}

	for _, addr := range info {
		parsedIP := net.ParseIP(addr)

		if parsedIP.To4() != nil {
			ipv4s = append(ipv4s, addr)
		} else if parsedIP.To16() != nil {
			ipv6s = append(ipv6s, "["+addr+"]")
		}
	}

	//Randomize the order of each
	rand.Shuffle(len(ipv4s), func(i, j int) { ipv4s[i], ipv4s[j] = ipv4s[j], ipv4s[i] })
	rand.Shuffle(len(ipv6s), func(i, j int) { ipv6s[i], ipv6s[j] = ipv6s[j], ipv6s[i] })

	// Always prefer IPv4
	return append(ipv4s, ipv6s...)

}

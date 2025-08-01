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

package client

import (
	"context"
	"encoding/json"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"github.com/pelicanplatform/pelican/config"
	"github.com/pelicanplatform/pelican/param"
	"github.com/pelicanplatform/pelican/pelican_url"
	"github.com/pelicanplatform/pelican/server_structs"
	"github.com/pelicanplatform/pelican/utils"
)

// Check whether an HTTP response is actually a response from a Pelican service, as
// indicated by the "Server" header pointing to a pelican process.
//
// We use this to handle retries in the event that the Director is down, but some ingress proxy in
// front of it is still answering requests with errant 404s, 500s, 502s, etc.
func fromPelican(resp *http.Response) bool {
	if param.Client_AssumeDirectorServerHeader.GetBool() {
		log.Debugln("Will assume response is from Director instead of checking for matching Server header. To change this behavior,",
			"set the 'Client.AssumeDirectorServerHeader' configuration option to false.")
		return true
	}
	return strings.HasPrefix(resp.Header.Get("Server"), "pelican/")
}

// Make a request to the director for a given verb/resource; return the
// HTTP response object only if a 307 is returned.
func queryDirector(ctx context.Context, verb string, pUrl *pelican_url.PelicanURL, token string) (resp *http.Response, err error) {
	resourceUrl, err := url.Parse(pUrl.FedInfo.DirectorEndpoint)
	if err != nil {
		log.Errorln("Failed to parse the director URL:", err)
		return nil, err
	}
	resourceUrl.Path = pUrl.Path
	resourceUrl.RawQuery = pUrl.RawQuery

	// Here we use http.Transport to prevent the client from following the director's
	// redirect. We use the Location url elsewhere (plus we still need to do the token
	// dance!)
	client := config.GetClientNoRedirect()

	var errMsg string
	var body []byte
	// The `fromDirector` variable indicates we think this response came from a director
	// process, not a proxy / ingress like traefik.
	var fromDirector bool
	// In case the director is momentarily down, we will retry a few times using a backoff strategy
	// I assume numRetries is >=1, which should enforced in config.go. However, not all tests that hit this code initialize the client.
	numRetries := param.Client_DirectorRetries.GetInt()
	if numRetries < 1 {
		log.Errorf("The config parameter %s is currently set to %d. This should not be possible. Will use fallback of 1 retry",
			param.Client_DirectorRetries.GetName(), numRetries)
		numRetries = 1
	}
	for idx := 0; idx < numRetries; idx++ {
		var req *http.Request
		req, err = http.NewRequestWithContext(ctx, verb, resourceUrl.String(), nil)
		if err != nil {
			log.Errorln("Failed to create an HTTP request:", err)
			return nil, err
		}

		// Include the Client's version as a User-Agent header. The Director will decide
		// if it supports the version, and provide an error message in the case that it
		// cannot.
		req.Header.Set("User-Agent", getUserAgent(""))

		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}

		if log.IsLevelEnabled(log.DebugLevel) {
			req.Header.Set("X-Pelican-Debug", "true")
		}

		// Perform the HTTP request
		resp, err = client.Do(req)

		if err != nil {
			log.Errorln("Failed to get response from the director:", err)
			return
		}

		defer resp.Body.Close()
		log.Tracef("Director's response: %#v\n", resp)
		// Check HTTP response -- should be 307 (redirect), else something went wrong
		body, err = io.ReadAll(resp.Body)
		if err != nil {
			log.Errorln("Failed to read the body from the director response:", err)
			return resp, err
		}
		errMsg = string(body)

		// If this isn't a Pelican process _and_ we got an error, sleep then retry. We may be talking
		// to something like a Traefik ingress controller that's waiting for the Director to come
		// back online.
		fromDirector = fromPelican(resp)
		if !fromDirector && (resp.StatusCode == http.StatusBadGateway || resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusInternalServerError) {
			if idx == 0 {
				log.Warnf("Response not from a Pelican process, the Director may be rebooting; will retry a total of %d times.", numRetries)
			}
			sleepFor := 3*idx + 3
			log.Warningln("Sleeping for", sleepFor, "seconds before retrying.")
			// backoff+randomness to avoid thundering herd
			time.Sleep(time.Duration(sleepFor)*time.Second + time.Duration(rand.Float32()*1000)*time.Millisecond)
		} else if fromDirector && resp.StatusCode == http.StatusTooManyRequests {
			// We just hit the Director after a reboot, but potentially before it's repopulated its
			// cache of server adds. Retry until we stop getting the 429 or we hit our limit.
			if idx == 0 {
				log.Warningln("The Director indicates it has just rebooted and is still discovering federation services.")
			}
			sleepFor := 3*idx + 3
			log.Warningln("Sleeping for", sleepFor, "seconds before retrying.")
			time.Sleep(time.Duration(sleepFor)*time.Second + time.Duration(rand.Float32()*1000)*time.Millisecond)
		} else {
			break
		}
	}

	// The Content-Type will be alike "application/json; charset=utf-8"
	if resp.StatusCode != http.StatusTemporaryRedirect && utils.HasContentType(resp, "application/json") {
		var respErr server_structs.SimpleApiResp
		if unmarshalErr := json.Unmarshal(body, &respErr); unmarshalErr != nil { // Error creating json
			log.Errorln("Failed to unmarshal the director's JSON response:", err)
			return resp, unmarshalErr
		}
		fromDirector = true
		// In case we have old director returning "error": "message content"
		if respErr.Msg != "" {
			errMsg = respErr.Msg
		}
	}

	bodyString := string(body)
	if resp.StatusCode == http.StatusMultiStatus && verb == "PROPFIND" {
		// This is a director >7.9 proxy the PROPFIND response instead of redirect to the origin
		return
	} else if resp.StatusCode != 307 {
		// Attempt to query the director using the PUT HTTP method instead of DELETE,
		// as older versions of the director may not support the DELETE endpoint.
		if resp.StatusCode == http.StatusNotFound && verb == http.MethodDelete {
			if strings.Contains(strings.ToLower(bodyString), "page not found") {
				log.Warningf("Failed to query the DELETE endpoint; the director appears to be an older version, attempting with the PUT method")
				return queryDirector(ctx, http.MethodPut, pUrl, token)
			}
		}
		if resp.StatusCode == http.StatusNotFound && fromDirector && (errMsg == "All sources report object was not found" || errMsg == "Object not found at any cache") {
			sce := StatusCodeError(http.StatusNotFound)
			err = &sce
		} else {
			err = errors.Errorf("%d: %s", resp.StatusCode, errMsg)
		}
		return resp, err
	}

	// A 307 may come with a body that contains the redirect choice information
	if bodyString != "" {
		log.Debugf("Director's redirect choice information: %s", bodyString)
	}

	return
}

type ServerPriority struct {
	URL      *url.URL
	Priority int
}

func parseServersFromDirectorResponse(resp *http.Response) (servers []*url.URL, err error) {
	linkHeader := resp.Header.Values("Link")
	if len(linkHeader) == 0 {
		return nil, nil
	}

	serversPrio := make([]ServerPriority, 0)
	for _, linksStr := range strings.Split(linkHeader[0], ",") {
		links := strings.Split(strings.ReplaceAll(linksStr, " ", ""), ";")

		var endpoint string
		// var rel string // "rel", as defined in the Metalink/HTTP RFC. Currently not being used by
		// the OSDF Client, but is provided by the director. Will be useful in the future when
		// we start looking at cases where we want to duplicate from caches if we're throttling
		// connections to the origin.
		var pri int
		for _, val := range links {
			if strings.HasPrefix(val, "<") {
				endpoint = val[1 : len(val)-1]
			} else if strings.HasPrefix(val, "pri") {
				pri, _ = strconv.Atoi(val[4:])
			}
			// } else if strings.HasPrefix(val, "rel") {
			// 	rel = val[5 : len(val)-1]
			// }
		}

		// Construct the cache objects, getting endpoint and auth requirements from
		// Director
		server, err := url.Parse(endpoint)
		if err != nil {
			log.Errorln("Failed to parse server:", endpoint, "error:", err)
			continue
		}
		serversPrio = append(serversPrio, ServerPriority{URL: server, Priority: pri})
	}

	// Making the assumption that the Link header doesn't already provide the caches
	// in order (even though it probably does). This sorts the caches and ensures
	// we're using the "pri" tag to order them
	sort.Slice(serversPrio, func(i, j int) bool {
		return serversPrio[i].Priority < serversPrio[j].Priority
	})

	servers = make([]*url.URL, len(serversPrio))
	for i, serverPrio := range serversPrio {
		servers[i] = serverPrio.URL
	}

	return
}

// Retrieve federation namespace information for a given URL.
func GetDirectorInfoForPath(ctx context.Context, pUrl *pelican_url.PelicanURL, httpMethod string, token string) (parsedResponse server_structs.DirectorResponse, err error) {
	if pUrl.FedInfo.DirectorEndpoint == "" {
		return server_structs.DirectorResponse{},
			errors.Errorf("unable to retrieve information from a Director for object %s because none was found in pelican URL metadata.", pUrl.Path)
	}

	log.Debugln("Will query director at", pUrl.FedInfo.DirectorEndpoint, "for object", pUrl.Path)

	var dirResp *http.Response
	dirResp, err = queryDirector(ctx, httpMethod, pUrl, token)
	if err != nil {
		if (httpMethod == http.MethodPut || httpMethod == http.MethodDelete) && dirResp != nil && dirResp.StatusCode == 405 {
			err = errors.Errorf("the director returned status code 405, indicating it understood the request but could not find an origin that supports PUT/DELETE operations for object: %s.", pUrl.Path)
			return
		} else {
			err = errors.Wrapf(err, "error while querying the director at %s", pUrl.FedInfo.DirectorEndpoint)
			return
		}
	}

	parsedResponse, err = ParseDirectorInfo(dirResp)
	if err != nil {
		err = errors.Wrap(err, "failed to parse director response")
		return
	}

	return
}

// Given the Director response, parse the headers and construct the ordered list of object
// servers.
func ParseDirectorInfo(dirResp *http.Response) (server_structs.DirectorResponse, error) {
	var xPelNs server_structs.XPelNs
	if err := (&xPelNs).ParseRawResponse(dirResp); err != nil {
		return server_structs.DirectorResponse{}, errors.Wrapf(err, "failed to parse %s header", xPelNs.GetName())
	}
	log.Debugln("Namespace path constructed from Director:", xPelNs.Namespace)

	var xPelAuth server_structs.XPelAuth
	if err := (&xPelAuth).ParseRawResponse(dirResp); err != nil {
		return server_structs.DirectorResponse{}, errors.Wrapf(err, "failed to parse %s header", xPelAuth.GetName())
	}

	var xPelTokGen server_structs.XPelTokGen
	if err := (&xPelTokGen).ParseRawResponse(dirResp); err != nil {
		return server_structs.DirectorResponse{}, errors.Wrapf(err, "failed to parse %s header", xPelTokGen.GetName())
	}

	sortedObjectServers, err := parseServersFromDirectorResponse(dirResp)
	if err != nil {
		return server_structs.DirectorResponse{}, errors.Wrap(err, "failed to determine object servers from Director's response")
	}

	return server_structs.DirectorResponse{
		ObjectServers: sortedObjectServers,
		XPelAuthHdr:   xPelAuth,
		XPelNsHdr:     xPelNs,
		XPelTokGenHdr: xPelTokGen,
	}, nil
}

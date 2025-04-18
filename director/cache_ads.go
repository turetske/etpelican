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

package director

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/jellydator/ttlcache/v3"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"

	"github.com/pelicanplatform/pelican/config"
	"github.com/pelicanplatform/pelican/metrics"
	"github.com/pelicanplatform/pelican/param"
	"github.com/pelicanplatform/pelican/server_structs"
	"github.com/pelicanplatform/pelican/utils"
)

type filterType string

const (
	permFiltered   filterType = "permFiltered"     // Read from Director.FilteredServers
	tempFiltered   filterType = "tempFiltered"     // Filtered by web UI, e.g. the server is put in downtime via the director website
	serverFiltered filterType = "serverFiltered"   // Filtered by the server itself, e.g. the server is put in downtime by the server admin
	topoFiltered   filterType = "topologyFiltered" // Filtered by Topology, e.g. the server is put in downtime via the OSDF Topology change
	tempAllowed    filterType = "tempAllowed"      // Read from Director.FilteredServers but mutated by web UI
)

var (
	// The in-memory cache of xrootd server advertisement, with the key being ServerAd.URL.String()
	serverAds = ttlcache.New(ttlcache.WithTTL[string, *server_structs.Advertisement](15 * time.Minute))
	// The map holds servers that are disabled, with the key being the ServerAd.Name
	// The map should be idenpendent of serverAds as we want to persist this change in-memory, regardless of the presence of the serverAd
	filteredServers      = map[string]filterType{}
	filteredServersMutex = sync.RWMutex{}
)

func (f filterType) String() string {
	switch f {
	case permFiltered:
		return "Permanently Disabled via the director configuration"
	case tempFiltered:
		return "Temporarily disabled via the admin website"
	case serverFiltered:
		return "Temporarily disabled by the server admin"
	case topoFiltered:
		return "Disabled via the Topology policy"
	case tempAllowed:
		return "Temporarily enabled via the admin website"
	case "": // Here is to simplify the empty value at the UI side
		return ""
	default:
		return "Unknown Type"
	}
}

// recordAd does following for an incoming ServerAd and []NamespaceAdV2 pair:
//
//  1. Update the ServerAd by setting server location and updating server topology attribute
//  2. Record the ServerAd and NamespaceAdV2 to the TTL cache
//  3. Set up the server `stat` call utilities
//  4. Set up utilities for collecting origin/health server file transfer test status
//  5. Return the updated ServerAd. The ServerAd passed in will not be modified
func recordAd(ctx context.Context, sAd server_structs.ServerAd, namespaceAds *[]server_structs.NamespaceAdV2) (updatedAd server_structs.ServerAd) {
	if err := updateLatLong(&sAd); err != nil {
		if geoIPError, ok := err.(geoIPError); ok {
			labels := geoIPError.labels
			// TODO: Remove this metric (the line directly below)
			// The renamed metric was added in v7.16
			metrics.PelicanDirectorGeoIPErrors.With(labels).Inc()
			metrics.PelicanDirectorGeoIPErrorsTotal.With(labels).Inc()
		}
		log.Debugln("Failed to lookup GeoIP coordinates for host", sAd.URL.Host)
	}

	if sAd.URL.String() == "" {
		log.Errorf("The URL of the serverAd %#v is empty. Cannot set the TTL cache.", sAd)
		return
	}
	// Since servers from topology always use http, while servers from Pelican always use https
	// we want to ignore the scheme difference when checking duplicates (only consider hostname:port)
	rawURL := sAd.URL.String() // could be http (topology) or https (Pelican or some topology ones)
	httpURL := sAd.URL.String()
	httpsURL := sAd.URL.String()
	if strings.HasPrefix(rawURL, "https") {
		httpURL = "http" + strings.TrimPrefix(rawURL, "https")
	}
	if strings.HasPrefix(rawURL, "http://") {
		httpsURL = "https://" + strings.TrimPrefix(rawURL, "http://")
	}

	existing := serverAds.Get(httpURL)
	if existing == nil {
		existing = serverAds.Get(httpsURL)
	}
	if existing == nil {
		existing = serverAds.Get(rawURL)
	}

	// There's an existing ad in the cache
	if existing != nil {
		if sAd.FromTopology && !existing.Value().FromTopology {
			// if the incoming is from topology but the existing is from Pelican
			log.Debugf("The ServerAd generated from topology with name %s and URL %s was ignored because there's already a Pelican ad for this server", sAd.Name, sAd.URL.String())
			return
		}
		if !sAd.FromTopology && existing.Value().FromTopology {
			// Pelican server will overwrite topology one. We leave a message to let admin know
			log.Debugf("The existing ServerAd generated from topology with name %s and URL %s is replaced by the Pelican server with name %s", existing.Value().Name, existing.Value().URL.String(), sAd.Name)
			serverAds.Delete(existing.Value().URL.String())
		}
		if !sAd.FromTopology && !existing.Value().FromTopology { // Only copy the IO Load value for Pelican server
			sAd.IOLoad = existing.Value().GetIOLoad() // we copy the value from the existing serverAD to be consistent
		}
	}

	ad := server_structs.Advertisement{ServerAd: sAd, NamespaceAds: *namespaceAds}

	adTTL := time.Until(sAd.Expiration)
	if sAd.Expiration.IsZero() {
		adTTL = param.Director_AdvertisementTTL.GetDuration()
		// Handle unit tests that do not initialize default config
		if adTTL == 0 {
			log.Info(param.Director_AdvertisementTTL.GetName(), "is set to 0; increasing to 15 minutes")
			adTTL = 15 * time.Minute
		}
	} else if adTTL <= 0 {
		return
	}

	serverAds.Set(ad.URL.String(), &server_structs.Advertisement{ServerAd: sAd, NamespaceAds: *namespaceAds}, adTTL)

	// Prepare `stat` call utilities for all servers regardless of its source (topology or Pelican)
	func() {
		statUtilsMutex.Lock()
		defer statUtilsMutex.Unlock()
		statUtil, ok := statUtils[ad.URL.String()]
		if !ok || statUtil.Errgroup == nil {
			baseCtx, cancel := context.WithCancel(ctx)
			concLimit := param.Director_StatConcurrencyLimit.GetInt()
			// If the value is not set or negative, then we provide a modest default;
			// we don't want to permit an unbounded number of queries due to potential
			// memory usage.
			if concLimit <= 0 {
				log.Warningln("Concurrency limit 'Director.StatConcurrencyLimit' must be positive; ignoring value", concLimit, "and using 100 instead")
				concLimit = 100
			}
			statErrGrp := utils.Group{}
			statErrGrp.SetLimit(concLimit)
			cap := param.Director_CachePresenceCapacity.GetInt()
			// Ensure the capacity is a positive integer; zero indicates
			// "unbounded" (bad) and a negative value gets cast to uint64,
			// becoming an effectively unbounded number (also bad)
			if cap <= 0 {
				log.Warningln("Object presence cache limit 'Director.CachePresenceCapacity' must be positive; ignoring value", cap, "and using 100 instead")
				cap = 100
			}
			newUtil := serverStatUtil{
				Errgroup: &statErrGrp,
				Cancel:   cancel,
				Context:  baseCtx,
				ResultCache: ttlcache.New(
					ttlcache.WithTTL[string, *objectMetadata](param.Director_CachePresenceTTL.GetDuration()),
					ttlcache.WithDisableTouchOnHit[string, *objectMetadata](),
					ttlcache.WithCapacity[string, *objectMetadata](uint64(cap)),
				),
			}
			log.Debugln("Creating a new stat cache with capacity", cap, "for endpoint ", ad.URL.String())
			// The result cache TTL is stopped when the `serverAds` struct is evicted.  This  occurs
			// when the server is cleanly shut down, preventing this goroutine from leaking.
			go newUtil.ResultCache.Start()
			statUtils[ad.URL.String()] = &newUtil
		}
	}()

	// We don't have health tests for the topology servers so just return
	if ad.FromTopology {
		return sAd
	}

	if ad.DisableDirectorTest {
		log.Debugf("%s server %s at %s has DisableDirectorTest set. Skip health test for this server.", ad.Type, ad.Name, ad.URL.String())
		return
	}

	// Prepare and launch the director file transfer tests to the origins/caches if it's not from the topology AND it's not already been registered
	func() {
		healthTestUtilsMutex.Lock()
		defer healthTestUtilsMutex.Unlock()

		if existingUtil, ok := healthTestUtils[ad.URL.String()]; ok {
			// Existing registration
			if existingUtil != nil {
				if existingUtil.ErrGrp != nil {
					if existingUtil.ErrGrpContext.Err() != nil {
						// ErrGroup has been Done. Start a new one
						errgrp, errgrpCtx := errgroup.WithContext(ctx)
						cancelCtx, cancel := context.WithCancel(errgrpCtx)

						errgrp.SetLimit(1)
						healthTestUtils[ad.URL.String()] = &healthTestUtil{
							Cancel:        cancel,
							ErrGrp:        errgrp,
							ErrGrpContext: errgrpCtx,
							Status:        HealthStatusInit,
						}
						errgrp.Go(func() error {
							LaunchPeriodicDirectorTest(cancelCtx, sAd)
							return nil
						})
						log.Debugf("New director test suite issued for %s %s. Errgroup was evicted", string(ad.Type), ad.URL.String())
					} else {
						// Existing errorgroup still working
						cancelCtx, cancel := context.WithCancel(existingUtil.ErrGrpContext)
						started := existingUtil.ErrGrp.TryGo(func() error {
							LaunchPeriodicDirectorTest(cancelCtx, sAd)
							return nil
						})
						if !started {
							cancel()
							log.Debugf("New director test suite blocked for %s %s, existing test has been running", string(ad.Type), ad.URL.String())
						} else {
							log.Debugf("New director test suite issued for %s %s. Existing registration", string(ad.Type), ad.URL.String())
							existingUtil.Cancel()
							existingUtil.Cancel = cancel
						}
					}
				} else {
					log.Errorf("%s %s registration didn't start a new director test cycle: errgroup is nil", string(ad.Type), &ad.URL)
				}
			} else {
				log.Errorf("%s %s registration didn't start a new director test cycle: healthTestUtils item is nil", string(ad.Type), &ad.URL)
			}
		} else { // No healthTestUtils found, new registration
			errgrp, errgrpCtx := errgroup.WithContext(ctx)
			cancelCtx, cancel := context.WithCancel(errgrpCtx)

			errgrp.SetLimit(1)
			healthTestUtils[ad.URL.String()] = &healthTestUtil{
				Cancel:        cancel,
				ErrGrp:        errgrp,
				ErrGrpContext: errgrpCtx,
				Status:        HealthStatusInit,
			}
			errgrp.Go(func() error {
				LaunchPeriodicDirectorTest(cancelCtx, sAd)
				return nil
			})
		}
	}()

	return sAd
}

func updateLatLong(ad *server_structs.ServerAd) error {
	if ad == nil {
		return errors.New("Cannot provide a nil ad to UpdateLatLong")
	}
	hostname := strings.Split(ad.URL.Host, ":")[0]
	ip, err := net.LookupIP(hostname)
	if err != nil {
		return err
	}
	if len(ip) == 0 {
		return fmt.Errorf("unable to find an IP address for hostname %s", hostname)
	}
	addr, ok := netip.AddrFromSlice(ip[0])
	if !ok {
		return errors.New("Failed to create address object from IP")
	}
	// NOTE: If GeoIP resolution of this address fails, lat/long are set to 0.0 (the null lat/long)
	// This causes the server to be sorted to the end of the list whenever the Director requires distance-aware sorting.
	lat, long, _, err := getLatLong(addr)
	if err != nil {
		return err
	}
	ad.Latitude = lat
	ad.Longitude = long
	return nil
}

// Clears the in-memory cache of server ads
func ClearServerAds() {
	serverAds.DeleteAll()
}

// init registers the director ClearServerAds function with the config package
func init() {
	config.ClearServerAdsCallback = ClearServerAds
}

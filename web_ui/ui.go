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

package web_ui

import (
	"context"
	"crypto/tls"
	"embed"
	"fmt"
	builtin_log "log"
	"math/rand"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	ginprometheus "github.com/zsais/go-gin-prometheus"
	"go.uber.org/atomic"
	"golang.org/x/sync/errgroup"
	"golang.org/x/term"

	"github.com/pelicanplatform/pelican/config"
	"github.com/pelicanplatform/pelican/database"
	"github.com/pelicanplatform/pelican/metrics"
	"github.com/pelicanplatform/pelican/param"
	"github.com/pelicanplatform/pelican/server_structs"
	"github.com/pelicanplatform/pelican/server_utils"
	"github.com/pelicanplatform/pelican/token"
	"github.com/pelicanplatform/pelican/token_scopes"
)

var (

	//go:embed frontend/out/*
	webAssets         embed.FS
	serverPages       = []string{"director", "registry", "origin", "cache"}
	publicAccessPages = []string{"director", "registry"}      // UI pages that allow unauthenticated users to access.
	adminAccessPages  = []string{"config", "origin", "cache"} // UI pages that allow non-admin users to access. Note that this is different from "publicView" where unauthenticated users can access the page
)

const notFoundFilePath = "frontend/out/404/index.html"

func ServerHeaderMiddleware(ctx *gin.Context) {
	ctx.Writer.Header().Add("Server", "pelican/"+config.GetVersion())
}

type CreateApiTokenReq struct {
	Name       string   `json:"name"`
	Expiration string   `json:"expiration"` // RFC3339 format, if not provided or "never" or "", token will not expire
	Scopes     []string `json:"scopes"`
}

// Initialize a hot restart of the server
func hotRestartServer(ctx *gin.Context) {
	ctx.JSON(http.StatusOK, server_structs.SimpleApiResp{
		Status: server_structs.RespOK,
		Msg:    "Server hot restart initiated",
	})
	config.RestartFlag <- true
}

func getConfigValues(ctx *gin.Context) {
	user := ctx.GetString("User")
	if user == "" {
		ctx.JSON(http.StatusForbidden, server_structs.SimpleApiResp{
			Status: server_structs.RespFailed,
			Msg:    "Authentication required to visit this API",
		})
		return
	}
	rawConfig, err := param.UnmarshalConfig(viper.GetViper())
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, server_structs.SimpleApiResp{
			Status: server_structs.RespFailed,
			Msg:    "Failed to get the unmarshaled rawConfig",
		})
		return
	}

	ctx.JSON(http.StatusOK, rawConfig)
}

func updateConfigValues(ctx *gin.Context) {
	updatedConfig := param.Config{}
	updatedConfigMap := map[string]interface{}{}

	// Check if the request data is a valid config
	if err := ctx.ShouldBindBodyWith(&updatedConfig, binding.JSON); err != nil {
		ctx.AbortWithStatusJSON(http.StatusBadRequest, server_structs.SimpleApiResp{
			Status: server_structs.RespFailed,
			Msg:    "Failed to bind the request. Invalid request data format: " + err.Error(),
		})
		return
	}
	if err := ctx.ShouldBindBodyWith(&updatedConfigMap, binding.JSON); err != nil {
		ctx.AbortWithStatusJSON(http.StatusBadRequest, server_structs.SimpleApiResp{
			Status: server_structs.RespFailed,
			Msg:    "Failed to bind the request into a map: " + err.Error(),
		})
		return
	}

	webConfigPath := param.Server_WebConfigFile.GetString()
	if webConfigPath == "" {
		ctx.AbortWithStatusJSON(http.StatusInternalServerError, server_structs.SimpleApiResp{
			Status: server_structs.RespFailed,
			Msg:    "Bad server configuration: Server.WebConfigFile value is empty",
		})
		return
	}

	// Create a new viper instance to handle config validation and merging
	webCfgViper := viper.New()
	webCfgViper.SetConfigFile(webConfigPath)

	if err := webCfgViper.ReadInConfig(); err != nil {
		log.Error("Failed to read existing web-based config into internal config struct: ", err.Error())
		ctx.AbortWithStatusJSON(http.StatusInternalServerError, server_structs.SimpleApiResp{
			Status: server_structs.RespFailed,
			Msg:    "Failed to read existing web-based config into internal config struct",
		})
		return
	}

	if err := webCfgViper.MergeConfigMap(updatedConfigMap); err != nil {
		log.Error("Failed to update web-based config with requested changes: ", err.Error())
		ctx.AbortWithStatusJSON(http.StatusInternalServerError, server_structs.SimpleApiResp{
			Status: server_structs.RespFailed,
			Msg:    "Failed to update web-based config with requested changes",
		})
		return
	}

	if err := webCfgViper.WriteConfig(); err != nil {
		log.Error("Failed to write back the updated config: ", err.Error())
		ctx.AbortWithStatusJSON(http.StatusInternalServerError, server_structs.SimpleApiResp{
			Status: server_structs.RespFailed,
			Msg:    "Failed to write back the updated config",
		})
		return
	}

	ctx.JSON(http.StatusOK,
		server_structs.SimpleApiResp{
			Status: server_structs.RespOK,
			Msg:    "success",
		})
	config.RestartFlag <- true
}

func getEnabledServers(ctx *gin.Context) {
	enabledServers := config.GetEnabledServerString(true)
	if len(enabledServers) == 0 {
		ctx.JSON(500, server_structs.SimpleApiResp{
			Status: server_structs.RespFailed,
			Msg:    "No enabled servers found",
		})
		return
	}

	ctx.JSON(200, gin.H{"servers": enabledServers})
}

func getVersionHandler(ctx *gin.Context) {
	response := gin.H{
		"version":     config.GetVersion(),
		"buildDate":   config.GetBuiltDate(),
		"buildCommit": config.GetBuiltCommit(),
		"builtBy":     config.GetBuiltBy(),
	}

	ctx.JSON(http.StatusOK, response)
}

func handleGlobusPages(ctx *gin.Context) {
	// /foo/bar
	requestPath := ctx.Param("requestPath")
	if strings.HasPrefix(requestPath, "/origin/globus") {
		// Attempt to visit Globus backend pages
		st := param.Origin_StorageType.GetString()
		if st != "globus" {
			// render 404 page
			file, _ := webAssets.ReadFile(notFoundFilePath)
			ctx.Data(
				http.StatusNotFound,
				mime.TypeByExtension(notFoundFilePath),
				file,
			)
			ctx.Abort()
			return
		}
		ctx.Next()
		return
	}
}

func handleWebUIRedirect(ctx *gin.Context) {
	query := ctx.Request.URL.Query().Encode()
	if query != "" {
		query = "?" + query
	}

	requestPath := ctx.Param("requestPath")

	// If the requestPath is a directory indicate that we are looking for the index.html file
	if strings.HasSuffix(requestPath, "/") {
		requestPath += "index.html"
	}

	// Clean the request path
	requestPath = path.Clean(requestPath)

	// If requestPath doesn't have extension, is not a directory, and has a index file, redirect to index file
	if !strings.Contains(requestPath, ".") && !strings.HasSuffix(requestPath, "/") {
		if _, err := webAssets.ReadFile("frontend/out" + requestPath + "/index.html"); err == nil {
			ctx.Redirect(http.StatusMovedPermanently, "/view/"+requestPath+"/"+query)
			ctx.Abort()
			return
		}
	}

	// If only one server is enabled, and user is requesting "/", redirect to that server
	if len(config.GetEnabledServerString(true)) == 1 && requestPath == "/index.html" {
		ctx.Redirect(http.StatusFound, "/view/"+config.GetEnabledServerString(true)[0]+"/"+query)
		ctx.Abort()
		return
	}

	// Only attempt to serve UI for servers enabled
	serverName := strings.Split(strings.TrimPrefix(requestPath, "/"), "/")[0]
	if slices.Contains(serverPages, serverName) {
		if (serverName == "director" && !config.IsServerEnabled(server_structs.DirectorType)) ||
			(serverName == "registry" && !config.IsServerEnabled(server_structs.RegistryType)) ||
			(serverName == "origin" && !config.IsServerEnabled(server_structs.OriginType)) ||
			(serverName == "cache" && !config.IsServerEnabled(server_structs.CacheType)) {
			file, _ := webAssets.ReadFile(notFoundFilePath)
			ctx.Data(
				http.StatusNotFound,
				mime.TypeByExtension(notFoundFilePath),
				file,
			)
			ctx.Abort()
			return
		}
	}
}

func handleWebUIAuth(ctx *gin.Context) {
	requestPath := ctx.Param("requestPath")
	db := authDB.Load()
	user, _, err := GetUserGroups(ctx)

	// Skip auth check for static files other than html pages
	if path.Ext(requestPath) != "" && path.Ext(requestPath) != ".html" {
		ctx.Next()
		return
	}

	// Handle initialization. If db is nill, then redirect user to the initialization page
	if strings.HasPrefix(requestPath, "/initialization") {
		if db != nil { // Password initialized, redirect away from the init page
			ctx.Redirect(http.StatusFound, "/view/")
			ctx.Abort()
			return
		} else { // If not init, pass the auth handler and render the page
			ctx.Next()
			return
		}
	} else if db == nil { // For all other paths, if the password is not initialized
		ctx.Redirect(http.StatusFound, "/view/initialization/code/")
		ctx.Abort()
		return
	}

	// Redirect authenticated users from login pages
	if strings.HasPrefix(requestPath, "/login") && err == nil && user != "" {
		returnUrl := ctx.Query("returnURL")
		if returnUrl == "" {
			returnUrl = "/view/"
		}
		ctx.Redirect(http.StatusFound, returnUrl)
		ctx.Abort()
		return
	}

	// For all routes other than /login and /initialization
	// / -> ""
	// /origin/ -> origin
	// /registry/origin/edit/ -> registry
	rootPage := strings.Split(strings.TrimPrefix(requestPath, "/"), "/")[0]

	// If the root is public, pass the check
	if slices.Contains(publicAccessPages, rootPage) {
		ctx.Next()
		return
	}

	// If user is not logged in
	if err != nil || user == "" {
		// If director or registry server is up and user is at /view/
		// then we allow them to choose the server without logging in
		if (config.IsServerEnabled(server_structs.DirectorType) ||
			config.IsServerEnabled(server_structs.RegistryType)) &&
			rootPage == "" {
			ctx.Next()
			return
		}
	}

	// If rootPage requires admin privilege
	if slices.Contains(adminAccessPages, rootPage) {
		isAdmin, _ := CheckAdmin(user)
		if isAdmin {

			// If user is admin, pass the check
			ctx.Next()
			return
		} else {

			// If user is not admin, rewrite the request to 403 page
			if err == nil && user != "" {

				ctx.Redirect(http.StatusFound, "/view/403/")
				ctx.Abort()
				return

				// If user is not logged in, redirect to login page
			} else {

				ctx.Redirect(http.StatusFound, "/view/login/?returnURL=/view"+url.QueryEscape(requestPath))
				ctx.Abort()
				return
			}
		}
	}

	// If it made it this far, it doesn't need special handling
	ctx.Next()
}

func handleWebUIResource(ctx *gin.Context) {
	requestPath := ctx.Param("requestPath")

	// If the requestPath is a directory indicate that we are looking for the index.html file
	if strings.HasSuffix(requestPath, "/") {
		requestPath += "index.html"
	}

	// Clean the request path
	requestPath = path.Clean(requestPath)

	filePath := "frontend/out" + requestPath
	file, _ := webAssets.ReadFile(filePath)

	// If the file is not found, return 404
	if file == nil {
		file, _ := webAssets.ReadFile(notFoundFilePath)
		ctx.Data(
			http.StatusNotFound,
			mime.TypeByExtension(path.Ext(notFoundFilePath)),
			file,
		)
	} else {
		// If the file is found, return the file
		ctx.Data(
			http.StatusOK,
			mime.TypeByExtension(path.Ext(filePath)),
			file,
		)
	}
}

func createApiToken(ctx *gin.Context) {
	authOption := token.AuthOption{
		Sources: []token.TokenSource{token.Cookie},
		Issuers: []token.TokenIssuer{token.LocalIssuer},
		Scopes:  []token_scopes.TokenScope{token_scopes.WebUi_Access},
	}

	status, ok, err := token.Verify(ctx, authOption)
	if !ok {
		log.Warningf("Cannot verify token: %v", err)
		ctx.JSON(status, server_structs.SimpleApiResp{
			Status: server_structs.RespFailed,
			Msg:    err.Error(),
		})
		return
	}
	// marshall body into struct
	var req CreateApiTokenReq
	err = ctx.ShouldBindJSON(&req)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, server_structs.SimpleApiResp{
			Status: server_structs.RespFailed,
			Msg:    fmt.Sprintf("Invalid request body: %v", err),
		})
		return
	}

	// check if all of the fields in the request are valid
	if req.Name == "" {
		ctx.JSON(http.StatusBadRequest, server_structs.SimpleApiResp{
			Status: server_structs.RespFailed,
			Msg:    "Name is required",
		})
		return
	}
	if len(req.Scopes) == 0 {
		ctx.JSON(http.StatusBadRequest, server_structs.SimpleApiResp{
			Status: server_structs.RespFailed,
			Msg:    "Scopes is required",
		})
		return
	}

	var expirationTime time.Time
	if req.Expiration == "" || req.Expiration == "never" { // if expiration is not provided, set token to never expire
		expirationTime = time.Time{}
	} else {
		expirationTime, err = time.Parse(time.RFC3339, req.Expiration)
		if err != nil {
			log.Warningf("Failed to parse expiration time: %v", err)
			ctx.JSON(http.StatusBadRequest, server_structs.SimpleApiResp{
				Status: server_structs.RespFailed,
				Msg:    "Expiration time was not given in RFC3339 format",
			})
			return
		}
		expirationTime = expirationTime.UTC()
	}
	scopes := strings.Join(req.Scopes, ",")
	user, _, err := GetUserGroups(ctx)
	if err != nil {
		log.Warn("Failed to get user from context")
		ctx.JSON(http.StatusInternalServerError, server_structs.SimpleApiResp{
			Status: server_structs.RespFailed,
			Msg:    "No user found when creating API key",
		})
		return
	}
	token, err := database.CreateApiKey(database.ServerDatabase, req.Name, user, scopes, expirationTime)
	if err != nil {
		log.Warning("Failed to create API key: ", err)
		ctx.JSON(status, server_structs.SimpleApiResp{
			Status: server_structs.RespFailed,
			Msg:    err.Error(),
		})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"token": token})
}

func deleteApiToken(ctx *gin.Context) {
	authOption := token.AuthOption{
		Sources: []token.TokenSource{token.Cookie},
		Issuers: []token.TokenIssuer{token.LocalIssuer},
		Scopes:  []token_scopes.TokenScope{token_scopes.WebUi_Access},
	}
	status, ok, err := token.Verify(ctx, authOption)
	if !ok {
		log.Warningf("Cannot verify token: %v", err)
		ctx.JSON(status, server_structs.SimpleApiResp{
			Status: server_structs.RespFailed,
			Msg:    err.Error(),
		})
		return
	}
	id := ctx.Param("id")
	err = database.DeleteApiKey(database.ServerDatabase, id, token.VerifiedKeysCache)
	if err != nil {
		log.Warning("Failed to delete API key: ", err)
		ctx.JSON(http.StatusInternalServerError, server_structs.SimpleApiResp{
			Status: server_structs.RespFailed,
			Msg:    err.Error(),
		})
		return
	}

	ctx.JSON(http.StatusOK, server_structs.SimpleApiResp{
		Status: server_structs.RespOK,
		Msg:    "API key deleted",
	})
}

func listApiTokens(ctx *gin.Context) {
	authOption := token.AuthOption{
		Sources: []token.TokenSource{token.Cookie},
		Issuers: []token.TokenIssuer{token.LocalIssuer},
		Scopes:  []token_scopes.TokenScope{token_scopes.WebUi_Access},
	}
	status, ok, err := token.Verify(ctx, authOption)
	if !ok {
		log.Warningf("Failed to verify WebUi Access Cookie: %v", err)
		ctx.JSON(status, server_structs.SimpleApiResp{
			Status: server_structs.RespFailed,
			Msg:    err.Error(),
		})
		return
	}

	apiKeys, err := database.ListApiKeys(database.ServerDatabase)
	if err != nil {
		log.Warning("Failed to list API keys: ", err)
		ctx.JSON(http.StatusInternalServerError, server_structs.SimpleApiResp{
			Status: server_structs.RespFailed,
			Msg:    err.Error(),
		})
		return
	}

	// Convert the API keys to the response format
	apiKeysResponse := make([]server_structs.ApiKeyResponse, len(apiKeys))
	for i, apiKey := range apiKeys {
		apiKeysResponse[i] = server_structs.ApiKeyResponse{
			ID:        apiKey.ID,
			Name:      apiKey.Name,
			Scopes:    strings.Split(apiKey.Scopes, ","),
			ExpiresAt: apiKey.ExpiresAt,
			CreatedAt: apiKey.CreatedAt,
			CreatedBy: apiKey.CreatedBy,
		}
	}

	ctx.JSON(http.StatusOK, apiKeysResponse)
}

func configureWebResource(engine *gin.Engine) {

	// Register the MIME type for .txt files
	err := mime.AddExtensionType(".txt", "text/plain")
	if err != nil {
		log.Errorf("Failed to register MIME type: %v", err)
	}

	engine.GET("/view/*requestPath",
		handleGlobusPages,
		handleWebUIRedirect,
		handleWebUIAuth,
		handleWebUIResource,
	)

	engine.GET("/api/v1.0/docs", func(ctx *gin.Context) {
		filePath := "frontend/out/api/docs/index.html"
		file, _ := webAssets.ReadFile(filePath)
		ctx.Data(
			http.StatusOK,
			mime.TypeByExtension(filePath),
			file,
		)
	})
}

// Configure common endpoint available to all server web UI which are located at /api/v1.0/*
func configureCommonEndpoints(engine *gin.Engine) error {
	engine.GET("/api/v1.0/config", AuthHandler, AdminAuthHandler, getConfigValues)
	engine.PATCH("/api/v1.0/config", AuthHandler, AdminAuthHandler, updateConfigValues)
	engine.POST("/api/v1.0/restart", AuthHandler, AdminAuthHandler, hotRestartServer)
	engine.GET("/api/v1.0/servers", getEnabledServers)
	// Health check endpoint for web engine
	engine.GET("/api/v1.0/health", func(ctx *gin.Context) {
		ctx.JSON(http.StatusOK, gin.H{"message": fmt.Sprintf("Web Engine Running. Time: %s", time.Now().String())})
	})
	engine.POST("/api/v1.0/tokens", AuthHandler, AdminAuthHandler, createApiToken)
	engine.DELETE("/api/v1.0/tokens/:id", AuthHandler, AdminAuthHandler, deleteApiToken)
	engine.GET("/api/v1.0/tokens", AuthHandler, AdminAuthHandler, listApiTokens)
	engine.GET("/api/v1.0/version", getVersionHandler)

	downtimeAPI := engine.Group("/api/v1.0/downtime")
	{
		downtimeAPI.POST("", AuthHandler, AdminAuthHandler, HandleCreateDowntime)
		downtimeAPI.GET("", HandleGetDowntime)
		downtimeAPI.GET("/:uuid", HandleGetDowntimeByUUID)
		downtimeAPI.PUT("/:uuid", AuthHandler, AdminAuthHandler, HandleUpdateDowntime)
		downtimeAPI.DELETE("/:uuid", AuthHandler, AdminAuthHandler, HandleDeleteDowntime)
	}

	return nil
}

// Map gin routes for Prometheus metrics to reduce metric cardinality
func mapPrometheusPath(c *gin.Context) string {
	url := c.Request.URL.Path
	// Frontend static resources
	if strings.HasPrefix(url, "/view/_next/") {
		url = "/view/_next/:resource"
		return url
	}
	if strings.HasPrefix(url, "/api/v1.0/director/healthTest/") {
		url = "/api/v1.0/director/healthTest/:testfile"
		return url
	}
	// Only keeps two level depth for object access
	if strings.HasPrefix(url, "/api/v1.0/director/object/") {
		objectPath := strings.TrimPrefix(path.Clean(url), "/api/v1.0/director/object/")
		twoLevels := strings.Split(objectPath, "/")
		if len(twoLevels) <= 2 {
			return url
		} else {
			aggPrefix := fmt.Sprintf("/%s/%s", twoLevels[0], twoLevels[1])
			url := "/api/v1.0/director/object" + aggPrefix + "/:path"
			return url
		}
	}
	// Only keeps two level depth for object access
	if strings.HasPrefix(url, "/api/v1.0/director/origin/") {
		objectPath := strings.TrimPrefix(path.Clean(url), "/api/v1.0/director/origin/")
		twoLevels := strings.Split(objectPath, "/")
		if len(twoLevels) <= 2 {
			return url
		} else {
			aggPrefix := fmt.Sprintf("/%s/%s", twoLevels[0], twoLevels[1])
			url := "/api/v1.0/director/origin" + aggPrefix + "/:path"
			return url
		}
	}
	return url
}

// Configure metrics related endpoints, including Prometheus and /health API
func configureMetrics(engine *gin.Engine) error {
	// Add authorization to /metric endpoint
	engine.Use(promMetricAuthHandler)

	prometheusMonitor := ginprometheus.NewPrometheus("gin")
	prometheusMonitor.ReqCntURLLabelMappingFn = mapPrometheusPath
	prometheusMonitor.Use(engine)

	// Health check endpoint for metrics
	healthFunc := func(ctx *gin.Context) {
		healthStatus := metrics.GetHealthStatus()
		ctx.JSON(http.StatusOK, healthStatus)
	}
	if param.Server_HealthMonitoringPublic.GetBool() {
		engine.GET("/api/v1.0/metrics/health", healthFunc)
	} else {
		engine.GET("/api/v1.0/metrics/health", AuthHandler, AdminAuthHandler, healthFunc)
	}
	return nil
}

// Send the one-time code for initial web UI login to stdout and periodically
// re-generate one-time code if user hasn't finished setup
func waitUntilLogin(ctx context.Context) error {
	if authDB.Load() != nil {
		return nil
	}
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	hostname := param.Server_Hostname.GetString()
	port := param.Server_WebPort.GetInt()
	isTTY := false
	if term.IsTerminal(int(os.Stdout.Fd())) {
		isTTY = true
		fmt.Printf("\n\n\n\n")
	}
	activationFile := param.Server_UIActivationCodeFile.GetString()

	defer func() {
		if err := os.Remove(activationFile); err != nil {
			log.Warningf("Failed to remove activation code file (%v): %v\n", activationFile, err)
		}
	}()
	for {
		previousCode.Store(currentCode.Load())
		newCode := fmt.Sprintf("%06v", rand.Intn(1000000))
		currentCode.Store(&newCode)
		newCodeWithNewline := fmt.Sprintf("%v\n", newCode)
		if err := os.WriteFile(activationFile, []byte(newCodeWithNewline), 0600); err != nil {
			log.Errorf("Failed to write activation code to file (%v): %v\n", activationFile, err)
		}

		if isTTY {
			fmt.Printf("\033[A\033[A\033[A\033[A")
			fmt.Printf("\033[2K\n")
			fmt.Printf("\033[2K\rPelican admin interface is not initialized\n\033[2KTo initialize, "+
				"login at \033[1;34mhttps://%v:%v/view/initialization/code/\033[0m with the following code:\n",
				hostname, port)
			fmt.Printf("\033[2K\r\033[1;34m%v\033[0m\n", *currentCode.Load())
		} else {
			fmt.Printf("Pelican admin interface is not initialized\n To initialize, login at https://%v:%v/view/initialization/code/ with the following code:\n", hostname, port)
			fmt.Println(*currentCode.Load())
		}
		start := time.Now()
		for time.Since(start) < 30*time.Second {
			select {
			case <-sigs:
				return errors.New("Process terminated...")
			case <-ctx.Done():
				return nil
			default:
				time.Sleep(100 * time.Millisecond)
			}
			if authDB.Load() != nil {
				return nil
			}
		}
	}
}

// Configure endpoints for server web APIs. This function does not configure any UI
// specific paths but just redirect root path to /view.
//
// You need to mount the static resources for UI in a separate function
func ConfigureServerWebAPI(ctx context.Context, engine *gin.Engine, egrp *errgroup.Group) error {
	// start the cache for verified API keys
	egrp.Go(func() error {
		token.VerifiedKeysCache.Start()
		return nil
	})

	// Wait on the context to stop the cache
	egrp.Go(func() error {
		<-ctx.Done()
		token.VerifiedKeysCache.Stop()
		return nil
	})

	if err := configureCommonEndpoints(engine); err != nil {
		return err
	}

	// Add pprof endpoints for debug purposes
	if param.Server_EnablePprof.GetBool() {
		configurePprof(engine)
	}

	if err := configureMetrics(engine); err != nil {
		return err
	}
	if param.Server_EnableUI.GetBool() {
		if err := configureAuthEndpoints(ctx, engine, egrp); err != nil {
			return err
		}
		configureWebResource(engine)
	}

	// Redirect root to /view for web UI
	engine.GET("/", func(c *gin.Context) {
		c.Redirect(http.StatusFound, "/view/")
	})
	return nil
}

// Setup the initial server web login by sending the one-time code to stdout
// and record health status of the WebUI based on the success of the initialization
func InitServerWebLogin(ctx context.Context) error {
	metrics.SetComponentHealthStatus(metrics.Server_WebUI, metrics.StatusWarning, "Authentication not initialized")

	if err := waitUntilLogin(ctx); err != nil {
		log.Errorln("Failure when waiting for web UI to be initialized:", err)
		return err
	}
	metrics.SetComponentHealthStatus(metrics.Server_WebUI, metrics.StatusOK, "")
	return nil
}

func GetEngine() (*gin.Engine, error) {
	gin.SetMode(gin.ReleaseMode)
	engine := gin.New()
	engine.Use(gin.Recovery())
	webLogger := log.WithFields(log.Fields{"daemon": "gin"})
	engine.Use(func(ctx *gin.Context) {
		startTime := time.Now()

		ctx.Next()

		latency := time.Since(startTime)
		webLogger.WithFields(log.Fields{"method": ctx.Request.Method,
			"status":   ctx.Writer.Status(),
			"time":     latency.String(),
			"client":   ctx.ClientIP(),
			"resource": ctx.Request.URL.Path},
		).Info("Served Request")
	})
	engine.HandleMethodNotAllowed = true
	return engine, nil
}

// Run the gin engine in the current goroutine.
//
// Will use a background golang routine to periodically reload the certificate
// utilized by the UI.
func RunEngine(ctx context.Context, engine *gin.Engine, egrp *errgroup.Group) error {
	return RunEngineRoutine(ctx, engine, egrp, true)
}

// Run the gin engine; if curRoutine is false, it will run in a background goroutine.
func RunEngineRoutine(ctx context.Context, engine *gin.Engine, egrp *errgroup.Group, curRoutine bool) error {
	addr := fmt.Sprintf("%v:%v", param.Server_WebHost.GetString(), param.Server_WebPort.GetInt())
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	config.UpdateConfigFromListener(ln)
	return RunEngineRoutineWithListener(ctx, engine, egrp, curRoutine, ln)
}

// Run the web engine connected to a provided listener `ln`.
func RunEngineRoutineWithListener(ctx context.Context, engine *gin.Engine, egrp *errgroup.Group, curRoutine bool, ln net.Listener) error {

	if curRoutine {
		defer ln.Close()
		return runEngineWithListener(ctx, ln, engine, egrp)
	} else {
		egrp.Go(func() error {
			defer ln.Close()
			return runEngineWithListener(ctx, ln, engine, egrp)
		})
		return nil
	}
}

// Run the engine with a given listener.
// This was split out from RunEngine to allow unit tests to provide a Unix domain socket'
// as a listener.
func runEngineWithListener(ctx context.Context, ln net.Listener, engine *gin.Engine, egrp *errgroup.Group) error {
	certFile := param.Server_TLSCertificateChain.GetString()
	keyFile := param.Server_TLSKey.GetString()

	port := param.Server_WebPort.GetInt()
	addr := fmt.Sprintf("%v:%v", param.Server_WebHost.GetString(), port)

	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		panic(err)
	}

	var certPtr atomic.Pointer[tls.Certificate]
	certPtr.Store(&cert)

	server_utils.LaunchWatcherMaintenance(
		ctx,
		[]string{filepath.Dir(param.Server_TLSCertificateChain.GetString())},
		"server TLS maintenance",
		2*time.Minute,
		func(notifyEvent bool) error {
			cert, err := tls.LoadX509KeyPair(certFile, keyFile)
			if err == nil {
				log.Debugln("Loaded new X509 key pair")
				certPtr.Store(&cert)
			} else if notifyEvent {
				log.Debugln("Failed to load new X509 key pair after filesystem event (may succeed eventually):", err)
				return nil
			}
			return err
		},
	)

	getCert := func(_ *tls.ClientHelloInfo) (*tls.Certificate, error) {
		return certPtr.Load(), nil
	}

	config := &tls.Config{
		GetCertificate: getCert,
	}
	logWriter := builtin_log.New(
		log.StandardLogger().WriterLevel(log.WarnLevel),
		"",
		0,
	)
	server := &http.Server{
		Addr:      addr,
		Handler:   engine.Handler(),
		TLSConfig: config,
		ErrorLog:  logWriter,
	}
	log.Debugln("Starting web engine at address", addr)

	// Once the context has been canceled, shutdown the HTTPS server.  Give it
	// 10 seconds to shutdown existing requests.
	egrp.Go(func() error {
		<-ctx.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err = server.Shutdown(ctx)
		if err != nil {
			log.Errorln("Failed to shutdown server:", err)
		}
		return err
	})

	if err := server.ServeTLS(ln, "", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}

	return nil
}

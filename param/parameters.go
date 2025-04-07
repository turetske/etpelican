// Code generated by go generate; DO NOT EDIT.
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

package param

import (
	"time"

	"github.com/spf13/viper"
)

type StringParam struct {
	name string
}

type StringSliceParam struct {
	name string
}

type BoolParam struct {
	name string
}

type IntParam struct {
	name string
}

type DurationParam struct {
	name string
}

type ObjectParam struct {
	name string
}

func GetDeprecated() map[string][]string {
    return map[string][]string{
        "Cache.DataLocation": {"Cache.StorageLocation"},
        "Cache.LocalRoot": {"Cache.StorageLocation"},
        "Director.EnableStat": {"Director.CheckOriginPresence"},
        "DisableHttpProxy": {"Client.DisableHttpProxy"},
        "DisableProxyFallback": {"Client.DisableProxyFallback"},
        "IssuerKey": {"none"},
        "Lotman.DbLocation": {"Lotman.LotHome"},
        "MinimumDownloadSpeed": {"Client.MinimumDownloadSpeed"},
        "Origin.EnableDirListing": {"Origin.EnableListings"},
        "Origin.EnableFallbackRead": {"Origin.EnableDirectReads"},
        "Origin.EnableWrite": {"Origin.EnableWrites"},
        "Origin.ExportVolume": {"Origin.ExportVolumes"},
        "Origin.Mode": {"Origin.StorageType"},
        "Origin.NamespacePrefix": {"Origin.FederationPrefix"},
        "Origin.S3ServiceName": {"none"},
        "Registry.AdminUsers": {"Server.UIAdminUsers"},
        "Server.TLSCertificate": {"Server.TLSCertificateChain"},
        "Xrootd.Port": {"Origin.Port", "Cache.Port"},
        "Xrootd.RunLocation": {"Cache.RunLocation", "Origin.RunLocation"},
    }
}

func (sP StringParam) GetString() string {
	return viper.GetString(sP.name)
}

func (sP StringParam) GetName() string {
	return sP.name
}

func (sP StringParam) IsSet() bool {
	return viper.IsSet(sP.name)
}

func (slP StringSliceParam) GetStringSlice() []string {
	return viper.GetStringSlice(slP.name)
}

func (slP StringSliceParam) GetName() string {
	return slP.name
}

func (slP StringSliceParam) IsSet() bool {
	return viper.IsSet(slP.name)
}

func (iP IntParam) GetInt() int {
	return viper.GetInt(iP.name)
}

func (iP IntParam) GetName() string {
	return iP.name
}

func (iP IntParam) IsSet() bool {
	return viper.IsSet(iP.name)
}

func (bP BoolParam) GetBool() bool {
	return viper.GetBool(bP.name)
}

func (bP BoolParam) GetName() string {
	return bP.name
}

func (bP BoolParam) IsSet() bool {
	return viper.IsSet(bP.name)
}

func (dP DurationParam) GetDuration() time.Duration {
	return viper.GetDuration(dP.name)
}

func (dP DurationParam) GetName() string {
	return dP.name
}

func (dP DurationParam) IsSet() bool {
	return viper.IsSet(dP.name)
}

func (oP ObjectParam) Unmarshal(rawVal any) error {
	return viper.UnmarshalKey(oP.name, rawVal)
}

func (oP ObjectParam) GetName() string {
	return oP.name
}

func (oP ObjectParam) IsSet() bool {
	return viper.IsSet(oP.name)
}

var (
	Cache_DataLocation = StringParam{"Cache.DataLocation"}
	Cache_DbLocation = StringParam{"Cache.DbLocation"}
	Cache_ExportLocation = StringParam{"Cache.ExportLocation"}
	Cache_FedTokenLocation = StringParam{"Cache.FedTokenLocation"}
	Cache_FilesBaseSize = StringParam{"Cache.FilesBaseSize"}
	Cache_FilesMaxSize = StringParam{"Cache.FilesMaxSize"}
	Cache_FilesNominalSize = StringParam{"Cache.FilesNominalSize"}
	Cache_HighWaterMark = StringParam{"Cache.HighWaterMark"}
	Cache_LocalRoot = StringParam{"Cache.LocalRoot"}
	Cache_LowWatermark = StringParam{"Cache.LowWatermark"}
	Cache_NamespaceLocation = StringParam{"Cache.NamespaceLocation"}
	Cache_RunLocation = StringParam{"Cache.RunLocation"}
	Cache_SentinelLocation = StringParam{"Cache.SentinelLocation"}
	Cache_StorageLocation = StringParam{"Cache.StorageLocation"}
	Cache_Url = StringParam{"Cache.Url"}
	Cache_XRootDPrefix = StringParam{"Cache.XRootDPrefix"}
	Director_AdvertiseUrl = StringParam{"Director.AdvertiseUrl"}
	Director_CacheSortMethod = StringParam{"Director.CacheSortMethod"}
	Director_DbLocation = StringParam{"Director.DbLocation"}
	Director_DefaultResponse = StringParam{"Director.DefaultResponse"}
	Director_GeoIPLocation = StringParam{"Director.GeoIPLocation"}
	Director_MaxMindKeyFile = StringParam{"Director.MaxMindKeyFile"}
	Director_SupportContactEmail = StringParam{"Director.SupportContactEmail"}
	Director_SupportContactUrl = StringParam{"Director.SupportContactUrl"}
	Federation_DiscoveryUrl = StringParam{"Federation.DiscoveryUrl"}
	Federation_TopologyDowntimeUrl = StringParam{"Federation.TopologyDowntimeUrl"}
	Federation_TopologyNamespaceUrl = StringParam{"Federation.TopologyNamespaceUrl"}
	Federation_TopologyUrl = StringParam{"Federation.TopologyUrl"}
	IssuerKey = StringParam{"IssuerKey"}
	IssuerKeysDirectory = StringParam{"IssuerKeysDirectory"}
	Issuer_AuthenticationSource = StringParam{"Issuer.AuthenticationSource"}
	Issuer_GroupFile = StringParam{"Issuer.GroupFile"}
	Issuer_GroupSource = StringParam{"Issuer.GroupSource"}
	Issuer_IssuerClaimValue = StringParam{"Issuer.IssuerClaimValue"}
	Issuer_OIDCAuthenticationUserClaim = StringParam{"Issuer.OIDCAuthenticationUserClaim"}
	Issuer_OIDCGroupClaim = StringParam{"Issuer.OIDCGroupClaim"}
	Issuer_QDLLocation = StringParam{"Issuer.QDLLocation"}
	Issuer_ScitokensServerLocation = StringParam{"Issuer.ScitokensServerLocation"}
	Issuer_TomcatLocation = StringParam{"Issuer.TomcatLocation"}
	LocalCache_DataLocation = StringParam{"LocalCache.DataLocation"}
	LocalCache_RunLocation = StringParam{"LocalCache.RunLocation"}
	LocalCache_Size = StringParam{"LocalCache.Size"}
	LocalCache_Socket = StringParam{"LocalCache.Socket"}
	Logging_Cache_Http = StringParam{"Logging.Cache.Http"}
	Logging_Cache_Ofs = StringParam{"Logging.Cache.Ofs"}
	Logging_Cache_Pfc = StringParam{"Logging.Cache.Pfc"}
	Logging_Cache_Pss = StringParam{"Logging.Cache.Pss"}
	Logging_Cache_PssSetOpt = StringParam{"Logging.Cache.PssSetOpt"}
	Logging_Cache_Scitokens = StringParam{"Logging.Cache.Scitokens"}
	Logging_Cache_Xrd = StringParam{"Logging.Cache.Xrd"}
	Logging_Cache_Xrootd = StringParam{"Logging.Cache.Xrootd"}
	Logging_Level = StringParam{"Logging.Level"}
	Logging_LogLocation = StringParam{"Logging.LogLocation"}
	Logging_Origin_Cms = StringParam{"Logging.Origin.Cms"}
	Logging_Origin_Http = StringParam{"Logging.Origin.Http"}
	Logging_Origin_Ofs = StringParam{"Logging.Origin.Ofs"}
	Logging_Origin_Oss = StringParam{"Logging.Origin.Oss"}
	Logging_Origin_Scitokens = StringParam{"Logging.Origin.Scitokens"}
	Logging_Origin_Xrd = StringParam{"Logging.Origin.Xrd"}
	Logging_Origin_Xrootd = StringParam{"Logging.Origin.Xrootd"}
	Lotman_DbLocation = StringParam{"Lotman.DbLocation"}
	Lotman_EnabledPolicy = StringParam{"Lotman.EnabledPolicy"}
	Lotman_LibLocation = StringParam{"Lotman.LibLocation"}
	Lotman_LotHome = StringParam{"Lotman.LotHome"}
	Monitoring_DataLocation = StringParam{"Monitoring.DataLocation"}
	OIDC_AuthorizationEndpoint = StringParam{"OIDC.AuthorizationEndpoint"}
	OIDC_ClientID = StringParam{"OIDC.ClientID"}
	OIDC_ClientIDFile = StringParam{"OIDC.ClientIDFile"}
	OIDC_ClientRedirectHostname = StringParam{"OIDC.ClientRedirectHostname"}
	OIDC_ClientSecretFile = StringParam{"OIDC.ClientSecretFile"}
	OIDC_DeviceAuthEndpoint = StringParam{"OIDC.DeviceAuthEndpoint"}
	OIDC_Issuer = StringParam{"OIDC.Issuer"}
	OIDC_TokenEndpoint = StringParam{"OIDC.TokenEndpoint"}
	OIDC_UserInfoEndpoint = StringParam{"OIDC.UserInfoEndpoint"}
	Origin_DbLocation = StringParam{"Origin.DbLocation"}
	Origin_ExportVolume = StringParam{"Origin.ExportVolume"}
	Origin_FedTokenLocation = StringParam{"Origin.FedTokenLocation"}
	Origin_FederationPrefix = StringParam{"Origin.FederationPrefix"}
	Origin_GlobusClientIDFile = StringParam{"Origin.GlobusClientIDFile"}
	Origin_GlobusClientSecretFile = StringParam{"Origin.GlobusClientSecretFile"}
	Origin_GlobusCollectionID = StringParam{"Origin.GlobusCollectionID"}
	Origin_GlobusCollectionName = StringParam{"Origin.GlobusCollectionName"}
	Origin_GlobusConfigLocation = StringParam{"Origin.GlobusConfigLocation"}
	Origin_HttpAuthTokenFile = StringParam{"Origin.HttpAuthTokenFile"}
	Origin_HttpServiceUrl = StringParam{"Origin.HttpServiceUrl"}
	Origin_Mode = StringParam{"Origin.Mode"}
	Origin_NamespacePrefix = StringParam{"Origin.NamespacePrefix"}
	Origin_RunLocation = StringParam{"Origin.RunLocation"}
	Origin_S3AccessKeyfile = StringParam{"Origin.S3AccessKeyfile"}
	Origin_S3Bucket = StringParam{"Origin.S3Bucket"}
	Origin_S3Region = StringParam{"Origin.S3Region"}
	Origin_S3SecretKeyfile = StringParam{"Origin.S3SecretKeyfile"}
	Origin_S3ServiceName = StringParam{"Origin.S3ServiceName"}
	Origin_S3ServiceUrl = StringParam{"Origin.S3ServiceUrl"}
	Origin_S3UrlStyle = StringParam{"Origin.S3UrlStyle"}
	Origin_ScitokensDefaultUser = StringParam{"Origin.ScitokensDefaultUser"}
	Origin_ScitokensNameMapFile = StringParam{"Origin.ScitokensNameMapFile"}
	Origin_ScitokensUsernameClaim = StringParam{"Origin.ScitokensUsernameClaim"}
	Origin_StoragePrefix = StringParam{"Origin.StoragePrefix"}
	Origin_StorageType = StringParam{"Origin.StorageType"}
	Origin_TokenAudience = StringParam{"Origin.TokenAudience"}
	Origin_Url = StringParam{"Origin.Url"}
	Origin_XRootDPrefix = StringParam{"Origin.XRootDPrefix"}
	Origin_XRootServiceUrl = StringParam{"Origin.XRootServiceUrl"}
	Plugin_Token = StringParam{"Plugin.Token"}
	Registry_DbLocation = StringParam{"Registry.DbLocation"}
	Registry_InstitutionsUrl = StringParam{"Registry.InstitutionsUrl"}
	Server_DbLocation = StringParam{"Server.DbLocation"}
	Server_ExternalWebUrl = StringParam{"Server.ExternalWebUrl"}
	Server_Hostname = StringParam{"Server.Hostname"}
	Server_IssuerHostname = StringParam{"Server.IssuerHostname"}
	Server_IssuerJwks = StringParam{"Server.IssuerJwks"}
	Server_IssuerUrl = StringParam{"Server.IssuerUrl"}
	Server_SessionSecretFile = StringParam{"Server.SessionSecretFile"}
	Server_TLSCACertificateDirectory = StringParam{"Server.TLSCACertificateDirectory"}
	Server_TLSCACertificateFile = StringParam{"Server.TLSCACertificateFile"}
	Server_TLSCAKey = StringParam{"Server.TLSCAKey"}
	Server_TLSCertificate = StringParam{"Server.TLSCertificate"}
	Server_TLSCertificateChain = StringParam{"Server.TLSCertificateChain"}
	Server_TLSKey = StringParam{"Server.TLSKey"}
	Server_UIActivationCodeFile = StringParam{"Server.UIActivationCodeFile"}
	Server_UIPasswordFile = StringParam{"Server.UIPasswordFile"}
	Server_UnprivilegedUser = StringParam{"Server.UnprivilegedUser"}
	Server_WebConfigFile = StringParam{"Server.WebConfigFile"}
	Server_WebHost = StringParam{"Server.WebHost"}
	Shoveler_AMQPExchange = StringParam{"Shoveler.AMQPExchange"}
	Shoveler_AMQPTokenLocation = StringParam{"Shoveler.AMQPTokenLocation"}
	Shoveler_MessageQueueProtocol = StringParam{"Shoveler.MessageQueueProtocol"}
	Shoveler_QueueDirectory = StringParam{"Shoveler.QueueDirectory"}
	Shoveler_StompCert = StringParam{"Shoveler.StompCert"}
	Shoveler_StompCertKey = StringParam{"Shoveler.StompCertKey"}
	Shoveler_StompPassword = StringParam{"Shoveler.StompPassword"}
	Shoveler_StompUsername = StringParam{"Shoveler.StompUsername"}
	Shoveler_Topic = StringParam{"Shoveler.Topic"}
	Shoveler_URL = StringParam{"Shoveler.URL"}
	StagePlugin_MountPrefix = StringParam{"StagePlugin.MountPrefix"}
	StagePlugin_OriginPrefix = StringParam{"StagePlugin.OriginPrefix"}
	StagePlugin_ShadowOriginPrefix = StringParam{"StagePlugin.ShadowOriginPrefix"}
	Xrootd_Authfile = StringParam{"Xrootd.Authfile"}
	Xrootd_ConfigFile = StringParam{"Xrootd.ConfigFile"}
	Xrootd_DetailedMonitoringHost = StringParam{"Xrootd.DetailedMonitoringHost"}
	Xrootd_LocalMonitoringHost = StringParam{"Xrootd.LocalMonitoringHost"}
	Xrootd_MacaroonsKeyFile = StringParam{"Xrootd.MacaroonsKeyFile"}
	Xrootd_ManagerHost = StringParam{"Xrootd.ManagerHost"}
	Xrootd_Mount = StringParam{"Xrootd.Mount"}
	Xrootd_RobotsTxtFile = StringParam{"Xrootd.RobotsTxtFile"}
	Xrootd_RunLocation = StringParam{"Xrootd.RunLocation"}
	Xrootd_ScitokensConfig = StringParam{"Xrootd.ScitokensConfig"}
	Xrootd_Sitename = StringParam{"Xrootd.Sitename"}
	Xrootd_SummaryMonitoringHost = StringParam{"Xrootd.SummaryMonitoringHost"}
)

var (
	Cache_DataLocations = StringSliceParam{"Cache.DataLocations"}
	Cache_MetaLocations = StringSliceParam{"Cache.MetaLocations"}
	Cache_PermittedNamespaces = StringSliceParam{"Cache.PermittedNamespaces"}
	ConfigLocations = StringSliceParam{"ConfigLocations"}
	Director_CacheResponseHostnames = StringSliceParam{"Director.CacheResponseHostnames"}
	Director_FilteredServers = StringSliceParam{"Director.FilteredServers"}
	Director_OriginResponseHostnames = StringSliceParam{"Director.OriginResponseHostnames"}
	Issuer_GroupRequirements = StringSliceParam{"Issuer.GroupRequirements"}
	Monitoring_AggregatePrefixes = StringSliceParam{"Monitoring.AggregatePrefixes"}
	Origin_ExportVolumes = StringSliceParam{"Origin.ExportVolumes"}
	Origin_ScitokensRestrictedPaths = StringSliceParam{"Origin.ScitokensRestrictedPaths"}
	Registry_AdminUsers = StringSliceParam{"Registry.AdminUsers"}
	Server_DirectorUrls = StringSliceParam{"Server.DirectorUrls"}
	Server_Modules = StringSliceParam{"Server.Modules"}
	Server_UIAdminUsers = StringSliceParam{"Server.UIAdminUsers"}
	Shoveler_OutputDestinations = StringSliceParam{"Shoveler.OutputDestinations"}
)

var (
	Cache_BlocksToPrefetch = IntParam{"Cache.BlocksToPrefetch"}
	Cache_Concurrency = IntParam{"Cache.Concurrency"}
	Cache_Port = IntParam{"Cache.Port"}
	Client_DirectorRetries = IntParam{"Client.DirectorRetries"}
	Client_MaximumDownloadSpeed = IntParam{"Client.MaximumDownloadSpeed"}
	Client_MinimumDownloadSpeed = IntParam{"Client.MinimumDownloadSpeed"}
	Client_WorkerCount = IntParam{"Client.WorkerCount"}
	Director_CachePresenceCapacity = IntParam{"Director.CachePresenceCapacity"}
	Director_MaxStatResponse = IntParam{"Director.MaxStatResponse"}
	Director_MinStatResponse = IntParam{"Director.MinStatResponse"}
	Director_StatConcurrencyLimit = IntParam{"Director.StatConcurrencyLimit"}
	LocalCache_HighWaterMarkPercentage = IntParam{"LocalCache.HighWaterMarkPercentage"}
	LocalCache_LowWaterMarkPercentage = IntParam{"LocalCache.LowWaterMarkPercentage"}
	MinimumDownloadSpeed = IntParam{"MinimumDownloadSpeed"}
	Monitoring_LabelLimit = IntParam{"Monitoring.LabelLimit"}
	Monitoring_LabelNameLengthLimit = IntParam{"Monitoring.LabelNameLengthLimit"}
	Monitoring_LabelValueLengthLimit = IntParam{"Monitoring.LabelValueLengthLimit"}
	Monitoring_PortHigher = IntParam{"Monitoring.PortHigher"}
	Monitoring_PortLower = IntParam{"Monitoring.PortLower"}
	Monitoring_SampleLimit = IntParam{"Monitoring.SampleLimit"}
	Origin_Port = IntParam{"Origin.Port"}
	Server_IssuerPort = IntParam{"Server.IssuerPort"}
	Server_UILoginRateLimit = IntParam{"Server.UILoginRateLimit"}
	Server_WebPort = IntParam{"Server.WebPort"}
	Shoveler_PortHigher = IntParam{"Shoveler.PortHigher"}
	Shoveler_PortLower = IntParam{"Shoveler.PortLower"}
	Transport_MaxIdleConns = IntParam{"Transport.MaxIdleConns"}
	Xrootd_DetailedMonitoringPort = IntParam{"Xrootd.DetailedMonitoringPort"}
	Xrootd_ManagerPort = IntParam{"Xrootd.ManagerPort"}
	Xrootd_Port = IntParam{"Xrootd.Port"}
	Xrootd_SummaryMonitoringPort = IntParam{"Xrootd.SummaryMonitoringPort"}
)

var (
	Cache_EnableLotman = BoolParam{"Cache.EnableLotman"}
	Cache_EnableOIDC = BoolParam{"Cache.EnableOIDC"}
	Cache_EnablePrefetch = BoolParam{"Cache.EnablePrefetch"}
	Cache_EnableTLSClientAuth = BoolParam{"Cache.EnableTLSClientAuth"}
	Cache_EnableVoms = BoolParam{"Cache.EnableVoms"}
	Cache_SelfTest = BoolParam{"Cache.SelfTest"}
	Client_AssumeDirectorServerHeader = BoolParam{"Client.AssumeDirectorServerHeader"}
	Client_DisableHttpProxy = BoolParam{"Client.DisableHttpProxy"}
	Client_DisableProxyFallback = BoolParam{"Client.DisableProxyFallback"}
	Client_IsPlugin = BoolParam{"Client.IsPlugin"}
	Debug = BoolParam{"Debug"}
	Director_AssumePresenceAtSingleOrigin = BoolParam{"Director.AssumePresenceAtSingleOrigin"}
	Director_CachesPullFromCaches = BoolParam{"Director.CachesPullFromCaches"}
	Director_CheckCachePresence = BoolParam{"Director.CheckCachePresence"}
	Director_CheckOriginPresence = BoolParam{"Director.CheckOriginPresence"}
	Director_EnableBroker = BoolParam{"Director.EnableBroker"}
	Director_EnableOIDC = BoolParam{"Director.EnableOIDC"}
	Director_EnableStat = BoolParam{"Director.EnableStat"}
	DisableHttpProxy = BoolParam{"DisableHttpProxy"}
	DisableProxyFallback = BoolParam{"DisableProxyFallback"}
	Issuer_OIDCPreferClaimsFromIDToken = BoolParam{"Issuer.OIDCPreferClaimsFromIDToken"}
	Issuer_UserStripDomain = BoolParam{"Issuer.UserStripDomain"}
	Logging_DisableProgressBars = BoolParam{"Logging.DisableProgressBars"}
	Lotman_EnableAPI = BoolParam{"Lotman.EnableAPI"}
	Monitoring_MetricAuthorization = BoolParam{"Monitoring.MetricAuthorization"}
	Monitoring_PromQLAuthorization = BoolParam{"Monitoring.PromQLAuthorization"}
	Origin_DirectorTest = BoolParam{"Origin.DirectorTest"}
	Origin_EnableBroker = BoolParam{"Origin.EnableBroker"}
	Origin_EnableCmsd = BoolParam{"Origin.EnableCmsd"}
	Origin_EnableDirListing = BoolParam{"Origin.EnableDirListing"}
	Origin_EnableDirectReads = BoolParam{"Origin.EnableDirectReads"}
	Origin_EnableFallbackRead = BoolParam{"Origin.EnableFallbackRead"}
	Origin_EnableIssuer = BoolParam{"Origin.EnableIssuer"}
	Origin_EnableListings = BoolParam{"Origin.EnableListings"}
	Origin_EnableMacaroons = BoolParam{"Origin.EnableMacaroons"}
	Origin_EnableOIDC = BoolParam{"Origin.EnableOIDC"}
	Origin_EnablePublicReads = BoolParam{"Origin.EnablePublicReads"}
	Origin_EnableReads = BoolParam{"Origin.EnableReads"}
	Origin_EnableUI = BoolParam{"Origin.EnableUI"}
	Origin_EnableVoms = BoolParam{"Origin.EnableVoms"}
	Origin_EnableWrite = BoolParam{"Origin.EnableWrite"}
	Origin_EnableWrites = BoolParam{"Origin.EnableWrites"}
	Origin_Multiuser = BoolParam{"Origin.Multiuser"}
	Origin_ScitokensMapSubject = BoolParam{"Origin.ScitokensMapSubject"}
	Origin_SelfTest = BoolParam{"Origin.SelfTest"}
	Registry_RequireCacheApproval = BoolParam{"Registry.RequireCacheApproval"}
	Registry_RequireKeyChaining = BoolParam{"Registry.RequireKeyChaining"}
	Registry_RequireOriginApproval = BoolParam{"Registry.RequireOriginApproval"}
	Server_DropPrivileges = BoolParam{"Server.DropPrivileges"}
	Server_EnablePprof = BoolParam{"Server.EnablePprof"}
	Server_EnableUI = BoolParam{"Server.EnableUI"}
	Server_HealthMonitoringPublic = BoolParam{"Server.HealthMonitoringPublic"}
	Shoveler_Enable = BoolParam{"Shoveler.Enable"}
	Shoveler_VerifyHeader = BoolParam{"Shoveler.VerifyHeader"}
	StagePlugin_Hook = BoolParam{"StagePlugin.Hook"}
	TLSSkipVerify = BoolParam{"TLSSkipVerify"}
	Xrootd_EnableLocalMonitoring = BoolParam{"Xrootd.EnableLocalMonitoring"}
)

var (
	Cache_DefaultCacheTimeout = DurationParam{"Cache.DefaultCacheTimeout"}
	Cache_SelfTestInterval = DurationParam{"Cache.SelfTestInterval"}
	Client_SlowTransferRampupTime = DurationParam{"Client.SlowTransferRampupTime"}
	Client_SlowTransferWindow = DurationParam{"Client.SlowTransferWindow"}
	Client_StoppedTransferTimeout = DurationParam{"Client.StoppedTransferTimeout"}
	Director_AdvertisementTTL = DurationParam{"Director.AdvertisementTTL"}
	Director_CachePresenceTTL = DurationParam{"Director.CachePresenceTTL"}
	Director_FedTokenLifetime = DurationParam{"Director.FedTokenLifetime"}
	Director_OriginCacheHealthTestInterval = DurationParam{"Director.OriginCacheHealthTestInterval"}
	Director_RegistryQueryInterval = DurationParam{"Director.RegistryQueryInterval"}
	Director_StatTimeout = DurationParam{"Director.StatTimeout"}
	Federation_TopologyReloadInterval = DurationParam{"Federation.TopologyReloadInterval"}
	Lotman_DefaultLotDeletionLifetime = DurationParam{"Lotman.DefaultLotDeletionLifetime"}
	Lotman_DefaultLotExpirationLifetime = DurationParam{"Lotman.DefaultLotExpirationLifetime"}
	Monitoring_DataRetention = DurationParam{"Monitoring.DataRetention"}
	Monitoring_TokenExpiresIn = DurationParam{"Monitoring.TokenExpiresIn"}
	Monitoring_TokenRefreshInterval = DurationParam{"Monitoring.TokenRefreshInterval"}
	Origin_SelfTestInterval = DurationParam{"Origin.SelfTestInterval"}
	Registry_InstitutionsUrlReloadMinutes = DurationParam{"Registry.InstitutionsUrlReloadMinutes"}
	Server_AdLifetime = DurationParam{"Server.AdLifetime"}
	Server_AdvertisementInterval = DurationParam{"Server.AdvertisementInterval"}
	Server_RegistrationRetryInterval = DurationParam{"Server.RegistrationRetryInterval"}
	Server_StartupTimeout = DurationParam{"Server.StartupTimeout"}
	Transport_DialerKeepAlive = DurationParam{"Transport.DialerKeepAlive"}
	Transport_DialerTimeout = DurationParam{"Transport.DialerTimeout"}
	Transport_ExpectContinueTimeout = DurationParam{"Transport.ExpectContinueTimeout"}
	Transport_IdleConnTimeout = DurationParam{"Transport.IdleConnTimeout"}
	Transport_ResponseHeaderTimeout = DurationParam{"Transport.ResponseHeaderTimeout"}
	Transport_TLSHandshakeTimeout = DurationParam{"Transport.TLSHandshakeTimeout"}
	Xrootd_AuthRefreshInterval = DurationParam{"Xrootd.AuthRefreshInterval"}
	Xrootd_MaxStartupWait = DurationParam{"Xrootd.MaxStartupWait"}
)

var (
	GeoIPOverrides = ObjectParam{"GeoIPOverrides"}
	Issuer_AuthorizationTemplates = ObjectParam{"Issuer.AuthorizationTemplates"}
	Issuer_OIDCAuthenticationRequirements = ObjectParam{"Issuer.OIDCAuthenticationRequirements"}
	Lotman_PolicyDefinitions = ObjectParam{"Lotman.PolicyDefinitions"}
	Origin_Exports = ObjectParam{"Origin.Exports"}
	Registry_CustomRegistrationFields = ObjectParam{"Registry.CustomRegistrationFields"}
	Registry_Institutions = ObjectParam{"Registry.Institutions"}
	Shoveler_IPMapping = ObjectParam{"Shoveler.IPMapping"}
)

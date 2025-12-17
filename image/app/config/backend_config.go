// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package config

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"integration/app/logging"
	"integration/app/plugin/impl/dataverse"
	"integration/app/plugin/types"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// Configuration types
type Config struct {
	DataverseServer string         `json:"dataverseServer"` // url of the server where Detaverse API is deployed
	RedisHost       string         `json:"redisHost"`       // redis host, not used when running the local/main.go (fake redis client with only 1 worker is used when running on local machine)
	GlobusEndpoint  string         `json:"globusEndpoint"`
	Options         OptionalConfig `json:"options"` // customizations
}

type OptionalConfig struct {
	DataverseExternalUrl         string        `json:"dataverseExternalUrl,omitempty"` // set this if different from dataverseServer -> this is used to generate a link to the dataset based
	RootDataverseId              string        `json:"rootDataverseId,omitempty"`      // root dataverse collection id, needed for creating new dataset when no collection was chosen in the UI (fallback to root collection)
	DefaultHash                  string        `json:"defaultHash,omitempty"`          // preset to md5, the default hash for most Dataverse installations, change this only when using a different hash (e.g., SHA-1)
	MyDataRoleIds                []int         `json:"myDataRoleIds"`                  // role ids that are sent with the "retrieve" my data api call
	PathToApiKey                 string        `json:"pathToApiKey,omitempty"`         // api (admin) API key is needed for URL signing. Configure the path to api key in this field to enable the URL signing.
	PathToUnblockKey             string        `json:"pathToUnblockKey,omitempty"`     // configure to enable checking permissions before requesting jobs
	PathToRedisPassword          string        `json:"pathToRedisPassword,omitempty"`  // by default no password for Redis is set, if you need to authenticate, store here the path to the file containing the redis password
	RedisDB                      int           `json:"redisDB,omitempty"`              // by default DB 0 is used, if you need to use other DB, specify it here
	DefaultDriver                string        `json:"defaultDriver,omitempty"`        // default driver as used by the dataverse installation, only "file" and "s3" are supported, leave empty otherwise
	StorageId                    string        `json:"storageId,omitempty"`            // storage identifier in Dataverse
	PathToFilesDir               string        `json:"pathToFilesDir,omitempty"`       // path to the folder where dataverse files are stored (only needed when using "file" driver)
	S3Config                     S3Config      `json:"s3Config"`                       // config if using "s3" driver -> see also settings for your s3 in Dataverse installation. Only needed when using S3 filesystem.
	PathToOauthSecrets           string        `json:"pathToOauthSecrets,omitempty"`   // path to file containing the oath client ids and secrets
	MaxFileSize                  int64         `json:"maxFileSize,omitempty"`          // if not set, the upload file size is unlimited
	UserHeaderName               string        `json:"userHeaderName,omitempty"`       // URL signing needs the username in order to know for which user to sign, the user name should be passed in the header of the request. The default is "Ajp_uid", as send by the Shibboleth IDP.
	SmtpConfig                   Smtp          `json:"smtpConfig"`                     // configure this when you wish to send notification emails to the users: on job error and on job completion
	PathToSmtpPassword           string        `json:"pathToSmtpPassword,omitempty"`   // path to the file containing the password needed to authenticate with the SMTP server
	MailConfig                   MailConfig    `json:"mailConfig"`
	MaxDvObjectPages             int           `json:"maxDvObjectPages"`
	PathToDataversePluginsConfig string        `json:"pathToDataversePluginsConfig"`
	ComputationQueues            []Queue       `json:"computationQueues"`
	ComputationAccessEndpoint    string        `json:"computationAccessEndpoint"`
	ComputationAccessConfig      []QueueAccess `json:"computationAccessConfig"`
	DisableDdiCdi                bool          `json:"disableDdiCdi,omitempty"` // set to true to disable DDI-CDI generation feature
	GlobusWebAppUrl              string        `json:"globusWebAppUrl,omitempty"`
	WorkspaceRoot                string        `json:"workspaceRoot,omitempty"`               // base directory for job workspaces (default: /dsdata)
	GlobusGuestDownloadUserName  string        `json:"globusGuestDownloadUserName,omitempty"` // when set, enables guest access for globus download (e.g., "GlobusDownloadOpenFiles")
	LoginRedirectUrl             string        `json:"loginRedirectUrl,omitempty"`            // URL to redirect unauthenticated users to login
}

type QueueAccess struct {
	UserEmail string   `json:"userEmail"`
	Queue     []string `json:"queue"`
}

type MailConfig struct {
	SubjectOnSuccess string `json:"subjectOnSuccess,omitempty"`
	ContentOnSuccess string `json:"contentOnSuccess,omitempty"`
	SubjectOnError   string `json:"subjectOnError,omitempty"`
	ContentOnError   string `json:"contentOnError,omitempty"`
}

type Smtp struct {
	Host string `json:"host,omitempty"`
	Port string `json:"port,omitempty"`
	From string `json:"from,omitempty"`
}

// Environment variables used for credentials: set these variables when using "s3" driver on the system where this application is deployed
// * Access Key ID:     AWS_ACCESS_KEY_ID or AWS_ACCESS_KEY
// * Secret Access Key: AWS_SECRET_ACCESS_KEY or AWS_SECRET_KEY
type S3Config struct {
	AWSEndpoint  string `json:"awsEndpoint"`
	AWSRegion    string `json:"awsRegion"`
	AWSPathstyle bool   `json:"awsPathstyle"`
	AWSBucket    string `json:"awsBucket"`
}

type OauthSecret struct {
	PostUrl      string `json:"postURL"`
	ClientSecret string `json:"clientSecret"`
	Resource     string `json:"resource"`
	Exchange     string `json:"exchange"`
}

var config Config
var oauthSecrets = map[string]OauthSecret{}
var queueAccess = map[string]map[string]bool{}

// static vars
var rdb RedisClient    // redis client singleton
var ApiKey = ""        // will be read from pathToApiKey
var UnblockKey = ""    // will be read from pathToUnblockKey
var redisPassword = "" // will be read from pathToRedisPassword
var SmtpPassword = ""  // will be read from pathToSmtpPassword
var AllowQuit = false
var LockMaxDuration = 168 * time.Hour

func init() {
	// read configuration
	configFile := os.Getenv("BACKEND_CONFIG_FILE")
	b, err := os.ReadFile(configFile)
	if err == nil {
		logging.Logger.Printf("using backend configuration from %v\n", configFile)
		err := json.Unmarshal(b, &config)
		if err != nil {
			panic(fmt.Errorf("config could not be loaded from %v: %v", configFile, err))
		}
	}
	if config.Options.DefaultHash == "" {
		config.Options.DefaultHash = types.Md5
	}

	// initialize variables
	b, err = os.ReadFile(config.Options.PathToUnblockKey)
	if err == nil {
		logging.Logger.Println("unblock key is read from file " + config.Options.PathToUnblockKey)
		UnblockKey = strings.TrimSpace(string(b))
	}

	b, err = os.ReadFile(config.Options.PathToApiKey)
	if err == nil {
		logging.Logger.Println("API key is read from file " + config.Options.PathToApiKey)
		ApiKey = strings.TrimSpace(string(b))
	}

	b, err = os.ReadFile(config.Options.PathToRedisPassword)
	if err == nil {
		logging.Logger.Println("redis password read from file " + config.Options.PathToRedisPassword)
		redisPassword = strings.TrimSpace(string(b))
	}

	b, err = os.ReadFile(config.Options.PathToOauthSecrets)
	if err == nil {
		err := json.Unmarshal(b, &oauthSecrets)
		if err == nil {
			logging.Logger.Println("OAUTH secrets read from file " + config.Options.PathToOauthSecrets)
		}
	}

	b, err = os.ReadFile(config.Options.PathToSmtpPassword)
	if err == nil {
		logging.Logger.Println("SMTP password is read from file " + config.Options.PathToSmtpPassword)
		SmtpPassword = strings.TrimSpace(string(b))
	}

	rdb = redis.NewClient(&redis.Options{
		Addr:     config.RedisHost,
		Password: redisPassword,
		DB:       config.Options.RedisDB,
	})
	if len(config.Options.MyDataRoleIds) == 0 {
		config.Options.MyDataRoleIds = []int{6, 7}
	}

	http.DefaultClient.Timeout = LockMaxDuration
	// allow bad certificates
	http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}

	// dataverse plugins config
	dvPluginsConfig := map[string]dataverse.Configuration{}
	b, err = os.ReadFile(config.Options.PathToDataversePluginsConfig)
	if err == nil {
		err := json.Unmarshal(b, &dvPluginsConfig)
		if err == nil {
			logging.Logger.Println("dataverse plugins config read from file " + config.Options.PathToDataversePluginsConfig)
		}
	}
	dataverse.Config = dvPluginsConfig

	for _, qa := range config.Options.ComputationAccessConfig {
		if queueAccess[qa.UserEmail] == nil {
			queueAccess[qa.UserEmail] = map[string]bool{}
		}
		for _, q := range qa.Queue {
			queueAccess[qa.UserEmail][q] = true
		}
	}
}

type RedisClient interface {
	Ping(ctx context.Context) *redis.StatusCmd
	Get(ctx context.Context, key string) *redis.StringCmd
	Set(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.StatusCmd
	Del(ctx context.Context, keys ...string) *redis.IntCmd
	SetNX(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.BoolCmd
	LPush(ctx context.Context, key string, values ...interface{}) *redis.IntCmd
	RPop(ctx context.Context, key string) *redis.StringCmd
}

func GetRedis() RedisClient {
	return rdb
}

func SetRedis(r RedisClient) {
	rdb = r
}

func SetConfig(dataverseServer, rootDataverseId, defaultHash string, roleIDs []int, allowQuit bool, maxFileSize int64) {
	config.DataverseServer = dataverseServer
	config.Options.RootDataverseId = rootDataverseId
	if defaultHash != "" {
		config.Options.DefaultHash = defaultHash
	}
	AllowQuit = allowQuit
	config.Options.MyDataRoleIds = roleIDs
	config.Options.MaxFileSize = maxFileSize
}

func RedisReady(ctx context.Context) bool {
	res, err := GetRedis().Ping(ctx).Result()
	if err != nil {
		logging.Logger.Printf("redis error: %v", err)
		return false
	}
	return res == "PONG"
}

func ClientSecret(clientId string) (clientSecret, resource, url, exchange string, err error) {
	s, ok := oauthSecrets[clientId]
	if !ok {
		return "", "", "", "", fmt.Errorf("OATH secret not found")
	}
	return s.ClientSecret, s.Resource, s.PostUrl, s.Exchange, nil
}

func GetMaxFileSize() int64 {
	return config.Options.MaxFileSize
}

func GetMaxDvObjectPages() int {
	return config.Options.MaxDvObjectPages
}

func GetConfig() Config {
	return config
}

func GetExternalDestinationURL() string {
	if config.Options.DataverseExternalUrl != "" {
		return config.Options.DataverseExternalUrl
	}
	return config.DataverseServer
}

func GetGlobusWebAppUrl() string {
	if config.Options.GlobusWebAppUrl != "" {
		return strings.TrimSuffix(config.Options.GlobusWebAppUrl, "/")
	}
	return "https://app.globus.org/activity"
}

func GetComputationQueues() []Queue {
	return config.Options.ComputationQueues
}

func HasAccessToQueue(userEmail, queue string) bool {
	if queue == "" {
		return len(queueAccess[userEmail]) > 0
	}
	return queueAccess[userEmail][queue]
}

func IsDdiCdiEnabled() bool {
	return !config.Options.DisableDdiCdi
}

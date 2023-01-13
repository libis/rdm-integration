package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"integration/app/logging"
	"integration/app/plugin/types"
	"os"
	"strings"
	"time"

	"github.com/go-redis/redis/v9"
)

// Configuration types
type Config struct {
	DataverseServer string         `json:"dataverseServer"` // url of the server where Detaverse API is deployed
	Options         OptionalConfig `json:"options"`         // customizations
}

type OptionalConfig struct {
	DataverseExternalUrl string   `json:"dataverseExternalUrl,omitempty"` // set this if different from dataverseServer -> this is used to generate a link to the dataset based
	RootDataverseId      string   `json:"rootDataverseId,omitempty"`      // root dataverse collection id, needed for creating new dataset when no collection was chosen in the UI (fallback to root collection)
	DefaultHash          string   `json:"defaultHash,omitempty"`          // default hash for most Dataverse installations, change this only when using a different hash (e.g., SHA-1)
	PathToUnblockKey     string   `json:"pathToUnblockKey,omitempty"`     // configure to enable checking permissions before requesting jobs
	RedisHost            string   `json:"redisHost,omitempty"`            // redis host, if left empty: sync map would be used instead (the workers and the webserver need to run in the same main in this case)
	PathToRedisPassword  string   `json:"pathToRedisPassword,omitempty"`  // by default no password is set, if you need to authenticate, store here the path to the file containing the redis password
	RedisDB              int      `json:"redisDB,omitempty"`              // by default DB 0 is used, if you need to use other DB, specify it here
	DefaultDriver        string   `json:"defaultDriver,omitempty"`        // default driver as used by the dataverse installation, only "file" and "s3" are supported, leave empty otherwise
	PathToFilesDir       string   `json:"pathToFilesDir,omitempty"`       // path to the folder where dataverse files are stored (only needed when using "file" driver)
	S3Config             S3Config `json:"s3Config,omitempty"`             // config if using "s3" driver -> see also settings for your s3 in Dataverse installation. Only needed when using S3 filesystem.
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

var config Config

// static vars
var rdb RedisClient                                                    // redis client singleton
var unblockKey = ""                                                    // will be read from pathToUnblockKey
var redisPassword = ""                                                 // will be read from pathToRedisPassword
var filesCleanup = "https://github.com/IQSS/dataverse/pull/9132"       // will be removed when pull request is merged
var directUpload = "https://github.com/IQSS/dataverse/pull/9003"       // will be removed when pull request is merged
var slashInPermissions = "https://github.com/IQSS/dataverse/pull/8995" // will be removed when pull request is merged

func init() {
	// read configuration
	configFile := os.Getenv("DATASYNC_CONFIG_FILE")
	b, err := os.ReadFile(configFile)
	if err != nil {
		logging.Logger.Printf("config file %v not found: letting the user to choose the server\n", configFile)
	} else {
		err := json.Unmarshal(b, &config)
		if err != nil {
			panic(fmt.Errorf("config confing could not be loaded from %v: %v", configFile, err))
		}
	}
	if config.Options.DefaultHash == "" {
		config.Options.DefaultHash = types.Md5
	}

	// initialize variables
	b, err = os.ReadFile(config.Options.PathToUnblockKey)
	if err != nil {
		logging.Logger.Println("unblock key could not be read from file " + config.Options.PathToUnblockKey + ": permissions will not be checked prior to requesting jobs: " + err.Error())
	} else {
		unblockKey = strings.TrimSpace(string(b))
	}

	b, err = os.ReadFile(config.Options.PathToRedisPassword)
	if err != nil {
		logging.Logger.Println("redis password could not be read from file " + config.Options.PathToRedisPassword + ": default empy password will be used: " + err.Error())
	} else {
		redisPassword = strings.TrimSpace(string(b))
	}

	rdb = redis.NewClient(&redis.Options{
		Addr:     config.Options.RedisHost,
		Password: redisPassword,
		DB:       config.Options.RedisDB,
	})
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

func RedisReady() bool {
	res, err := GetRedis().Ping(context.Background()).Result()
	if err != nil {
		logging.Logger.Printf("redis error: %v", err)
		return false
	}
	return res == "PONG"
}

package utils

import (
	"context"
	"integration/app/logging"
	"integration/app/plugin/types"
	"os"
	"strings"

	"github.com/go-redis/redis/v9"
)

// mandatory settings
var dataverseServer = "https://localhost:7000" // url of the server where Detaverse API is deployed
var redisHost = "localhost:6379"               // redis host where the jobs and requests can be stored
var rootDataverseId = "rdr"                    // root dataverse collection id
var defaultDriver = "file"                     // default driver as used by the dataverse installation

// config if using "file" driver
var pathToFilesDir = "../../rdm-deployment/data/dv/files/" // path to the folder where dataverse files are stored (only needed when using "file" driver)

// config if using "s3" driver -> see also settings for your s3 in Dataverse installation. Only needed when using S3 filesystem.
var awsEndpoint = "http://s3.libis.cloud"
var awsRegion = "libis-cloud"
var awsPathstyle = true
var awsBucket = "dataverse"

// Environment variables used for credentials: set these variables when using "s3" driver on the system where this application is deployed
// * Access Key ID:     AWS_ACCESS_KEY_ID or AWS_ACCESS_KEY
// * Secret Access Key: AWS_SECRET_ACCESS_KEY or AWS_SECRET_KEY

// optional settings
var dataverseExternalUrl = ""                                       // set this if different from dataverseServer -> this is used to generate a link to the dataset based
var defaultHash = types.Md5                                         // default hash for most Dataverse installations, change this only when using a different hash (e.g., SHA-1)
var pathToUnblockKey = "../../rdm-deployment/data/.secrets/api/key" // configure to enable checking permissions before requesting jobs
var pathToRedisPassword = ""                                        // no password set
var redisDB = 0                                                     // use default DB

// vars
var rdb *redis.Client                                                  // redis client singleton
var unblockKey = ""                                                    // will be read from pathToUnblockKey
var redisPassword = ""                                                 // will be read from pathToRedisPassword
var filesCleanup = "https://github.com/IQSS/dataverse/pull/9132"       // will be removed when pull request is merged
var directUpload = "https://github.com/IQSS/dataverse/pull/9003"       // will be removed when pull request is merged
var slashInPermissions = "https://github.com/IQSS/dataverse/pull/8995" // will be removed when pull request is merged

func init() {
	server := os.Getenv("DATAVERSE_SERVER")
	rh := os.Getenv("REDIS_HOST")
	dv := os.Getenv("ROOT_DATAVERSE")
	driver := os.Getenv("STORAGE_DRIVER")
	files := os.Getenv("FILES_PATH")
	region := os.Getenv("AWS_REGION")
	endpoint := os.Getenv("AWS_ENDPOINT")
	style := os.Getenv("AWS_PATH_STYLE_ACCESS")
	bucket := os.Getenv("AWS_BUCKET")
	url := os.Getenv("DV_EXT_URL")
	hash := os.Getenv("HASH_TYPE")
	pathUK := os.Getenv("PATH_TO_UNBLOCK_KEY")
	if server != "" {
		dataverseServer = server
	}
	if rh != "" {
		redisHost = rh
	}
	if dv != "" {
		rootDataverseId = dv
	}
	if driver != "" {
		defaultDriver = driver
	}
	if files != "" {
		pathToFilesDir = files
	}
	if region != "" {
		awsRegion = region
	}
	if endpoint != "" {
		awsEndpoint = endpoint
	}
	if style != "" {
		awsPathstyle = style == "true" || style == "TRUE" || style == "\"TRUE\"" || style == "\"true\""
	}
	if bucket != "" {
		awsBucket = bucket
	}
	if url != "" {
		dataverseExternalUrl = url
	}
	if hash != "" {
		defaultHash = hash
	}
	if pathUK != "" {
		pathToUnblockKey = pathUK
	}
	b, err := os.ReadFile(pathToUnblockKey)
	if err != nil {
		logging.Logger.Println("unblock key could not be read from file " + pathToUnblockKey + ": permissions will not be checked prior to requesting jobs: " + err.Error())
	} else {
		unblockKey = strings.TrimSpace(string(b))
	}
	b, err = os.ReadFile(pathToRedisPassword)
	if err != nil {
		logging.Logger.Println("redis password could not be read from file " + pathToRedisPassword + ": default empy password will be used: " + err.Error())
	} else {
		redisPassword = strings.TrimSpace(string(b))
	}

	rdb = redis.NewClient(&redis.Options{
		Addr:     redisHost,
		Password: redisPassword,
		DB:       redisDB,
	})
}

func GetRedis() *redis.Client {
	return rdb
}

func RedisReady() bool {
	res, err := rdb.Ping(context.Background()).Result()
	if err != nil {
		logging.Logger.Printf("redis error: %v", err)
		return false
	}
	return res == "PONG"
}

package utils

import (
	"context"
	"integration/app/logging"
	"integration/app/plugin/types"
	"os"
	"strings"

	"github.com/go-redis/redis/v9"
)

var pathToFilesDir = "../../rdm-deployment/data/dv/files/"
var dataverseServer = "https://localhost:7000"
var defaultDriver = "file"
var awsRegion = "libis-cloud"
var awsEndpoint = "http://s3.libis.cloud"
var awsPathstyle = true
var awsBucket = "dataverse"
var defaultHash = types.Md5
var pathToUnblockKey = "../../rdm-deployment/data/.secrets/api/key"
var unblockKey = "" //will be read from pathToUnblockKey
var redisHost = "localhost:6379"
var defaultDataverse = "rdr"
var FileServerPath = "../../rdm-integration-frontend/dist/datasync"
var slashInPermissions = "https://github.com/IQSS/dataverse/pull/8995"
var filesCleanup = "https://github.com/IQSS/dataverse/pull/9132"
var directUpload = "https://github.com/IQSS/dataverse/pull/9003"

var rdb *redis.Client

func init() {
	files := os.Getenv("FILES_PATH")
	server := os.Getenv("DATAVERSE_SERVER")
	driver := os.Getenv("STORAGE_DRIVER")
	region := os.Getenv("AWS_REGION")
	endpoint := os.Getenv("AWS_ENDPOINT")
	style := os.Getenv("AWS_PATH_STYLE_ACCESS")
	bucket := os.Getenv("AWS_BUCKET")
	hash := os.Getenv("HASH_TYPE")
	pathUK := os.Getenv("PATH_TO_UNBLOCK_KEY")
	rh := os.Getenv("REDIS_HOST")
	dv := os.Getenv("DEFAULT_DATAVERSE")
	fs := os.Getenv("FILE_SERVER_PATH")
	slash := os.Getenv("SLASH_IN_PERMISSIONS")
	cleanup := os.Getenv("FILES_CLEANUP")
	upload := os.Getenv("DIRECT_UPLOAD")
	// Environment variables used for credentials:
	// * Access Key ID:     AWS_ACCESS_KEY_ID or AWS_ACCESS_KEY
	// * Secret Access Key: AWS_SECRET_ACCESS_KEY or AWS_SECRET_KEY
	if files != "" {
		pathToFilesDir = files
	}
	if server != "" {
		dataverseServer = server
	}
	if driver != "" {
		defaultDriver = driver
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
	if hash != "" {
		defaultHash = hash
	}
	if pathUK != "" {
		pathToUnblockKey = pathUK
	}
	b, err := os.ReadFile(pathToUnblockKey)
	if err != nil {
		panic(err)
	}
	unblockKey = strings.TrimSpace(string(b))
	if rh != "" {
		redisHost = rh
	}
	if dv != "" {
		defaultDataverse = dv
	}
	if fs != "" {
		FileServerPath = fs
	}
	if slash != "" {
		slashInPermissions = slash
	}
	if cleanup != "" {
		filesCleanup = cleanup
	}
	if upload != "" {
		directUpload = upload
	}

	rdb = redis.NewClient(&redis.Options{
		Addr:     redisHost,
		Password: "", // no password set
		DB:       0,  // use default DB
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

package utils

import (
	"crypto/md5"
	"crypto/sha1"
	"fmt"
	"hash"
	"integration/app/tree"
	"io"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/google/uuid"
)

const (
	SHA1    = "SHA-1"
	GitHash = "git-hash"
	Md5     = "MD5"
)

type storage struct {
	driver   string
	bucket   string
	filename string
}

type hashingReader struct {
	reader io.Reader
	hasher hash.Hash
}

func (r hashingReader) Read(buf []byte) (n int, err error) {
	n, err = r.reader.Read(buf)
	r.hasher.Write(buf[:n])
	return
}

func getStorage(storageIdentifier string) storage {
	driver := ""
	filename := ""
	bucket := ""
	first := strings.Split(storageIdentifier, "://")
	second := []string{}
	if len(first) == 2 {
		driver = first[0]
		filename = first[1]
		second = strings.Split(filename, ":")
		if len(second) == 2 {
			bucket = second[0]
			filename = second[1]
		}
	}
	return storage{driver, bucket, filename}
}

func generateFileName() string {
	uid := uuid.New()
	hexRandom := uid[len(uid)-6:]
	hexTimestamp := time.Now().UnixMilli()
	return fmt.Sprintf("%x-%x", hexTimestamp, hexRandom)
}

func generateStorageIdentifier(fileName string) string {
	b := ""
	if defaultDriver == "s3" {
		b = awsBucket + ":"
	}
	return fmt.Sprintf("%s://%s%s", defaultDriver, b, fileName)
}

func getHash(hashType string, fileSize int) (hasher hash.Hash, err error) {
	if hashType == Md5 {
		hasher = md5.New()
	} else if hashType == SHA1 {
		hasher = sha1.New()
	} else if hashType == GitHash {
		hasher = sha1.New()
		hasher.Write([]byte(fmt.Sprintf("blob %d\x00", fileSize)))
	} else {
		err = fmt.Errorf("unsupported hash type: %v", hashType)
	}
	return
}

func write(fileStream stream, storageIdentifier, doi, hashType, remoteHashType string, fileSize int) ([]byte, []byte, error) {
	s := getStorage(storageIdentifier)
	hasher, err := getHash(hashType, fileSize)
	if err != nil {
		return nil, nil, err
	}
	remoteHasher, err := getHash(remoteHashType, fileSize)
	if err != nil {
		return nil, nil, err
	}
	reader := hashingReader{fileStream.Open(), hasher}
	reader = hashingReader{reader, remoteHasher}
	defer fileStream.Close()

	//TODO: stop stream and cleanup on Stop -> return error "stopped"
	if s.driver == "file" {
		file := pathToFilesDir + doi + "/" + s.filename
		f, err := os.Create(file)
		if err != nil {
			return nil, nil, err
		}
		defer f.Close()
		buf := make([]byte, 1024)
		for {
			n, err2 := reader.Read(buf)
			if err2 == io.EOF {
				break
			}
			f.Write(buf[:n])
		}
	} else if s.driver == "s3" {
		sess, err := session.NewSession(&aws.Config{
			Region:           aws.String(awsRegion),
			Endpoint:         aws.String(awsEndpoint),
			Credentials:      credentials.NewEnvCredentials(),
			S3ForcePathStyle: aws.Bool(awsPathstyle),
		})
		if err != nil {
			return nil, nil, err
		}
		uploader := s3manager.NewUploader(sess)
		_, err = uploader.Upload(&s3manager.UploadInput{
			Bucket: aws.String(s.bucket),
			Key:    aws.String(doi + "/" + s.filename),
			Body:   reader,
		})
		if err != nil {
			return nil, nil, err
		}
	} else {
		return nil, nil, fmt.Errorf("unsupported driver: %s", s.driver)
	}

	return hasher.Sum(nil), remoteHasher.Sum(nil), nil
}

func doHash(doi string, node tree.Node) ([]byte, error) {
	storageIdentifier := node.Attributes.Metadata.DataFile.StorageIdentifier
	hashType := node.Attributes.RemoteHashType
	hasher, err := getHash(hashType, node.Attributes.Metadata.DataFile.Filesize)
	if err != nil {
		return nil, err
	}
	s := getStorage(storageIdentifier)
	var reader io.Reader
	if s.driver == "file" {
		file := pathToFilesDir + doi + "/" + s.filename
		f, err := os.Open(file)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		reader = f
	} else if s.driver == "s3" {
		sess, _ := session.NewSession(&aws.Config{
			Region:           aws.String(awsRegion),
			Endpoint:         aws.String(awsEndpoint),
			Credentials:      credentials.NewEnvCredentials(),
			S3ForcePathStyle: aws.Bool(awsPathstyle),
		})
		svc := s3.New(sess)
		rawObject, err := svc.GetObject(
			&s3.GetObjectInput{
				Bucket: aws.String(s.bucket),
				Key:    aws.String(doi + "/" + s.filename),
			})
		if err != nil {
			return nil, err
		}
		defer rawObject.Body.Close()
		reader = rawObject.Body
	} else {
		return nil, fmt.Errorf("unsupported driver: %s", s.driver)
	}

	r := hashingReader{reader, hasher}
	buf := make([]byte, 1024)
	for {
		_, err2 := r.Read(buf)
		if err2 == io.EOF {
			break
		}
	}
	return hasher.Sum(nil), nil
}

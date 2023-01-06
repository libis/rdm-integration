package utils

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha1"
	"encoding/binary"
	"errors"
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
	SHA1     = "SHA-1"
	GitHash  = "git-hash"
	Md5      = "MD5"
	FileSize = "FileSize"
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
	} else if hashType == FileSize {
		hasher = newFileSizeHash(int64(fileSize))
	} else {
		err = fmt.Errorf("unsupported hash type: %v", hashType)
	}
	return
}

func newFileSizeHash(fileSize int64) hash.Hash {
	return FileSizeHash{FileSize: fileSize}
}

type FileSizeHash struct {
	FileSize int64
}

// Write (via the embedded io.Writer interface) adds more data to the running hash.
// It never returns an error.
func (h FileSizeHash) Write(p []byte) (n int, err error) {
	return len(p), nil
}

// Sum appends the current hash to b and returns the resulting slice.
// It does not change the underlying hash state.
func (h FileSizeHash) Sum(b []byte) []byte {
	res := make([]byte, 8)
	binary.LittleEndian.PutUint64(res, uint64(h.FileSize))
	return res
}

// Reset resets the Hash to its initial state.
func (h FileSizeHash) Reset() {}

// Size returns the number of bytes Sum will return.
func (h FileSizeHash) Size() int {
	return 8
}

// BlockSize returns the hash's underlying block size.
// The Write method must be able to accept any amount
// of data, but it may operate more efficiently if all writes
// are a multiple of the block size.
func (h FileSizeHash) BlockSize() int {
	return 256
}

func write(ctx context.Context, fileStream stream, storageIdentifier, persistentId, hashType, remoteHashType, id string, fileSize int) ([]byte, []byte, *bytes.Buffer, error) {
	b := bytes.NewBuffer(nil)
	pid, err := trimProtocol(persistentId)
	if err != nil {
		return nil, nil, nil, err
	}
	s := getStorage(storageIdentifier)
	hasher, err := getHash(hashType, fileSize)
	if err != nil {
		return nil, nil, nil, err
	}
	remoteHasher, err := getHash(remoteHashType, fileSize)
	if err != nil {
		return nil, nil, nil, err
	}
	readStream, err := fileStream.Open()
	defer fileStream.Close()
	if err != nil {
		return nil, nil, nil, err
	}
	reader := hashingReader{readStream, hasher}
	reader = hashingReader{reader, remoteHasher}

	if s.driver == "file" || directUpload != "true" {
		f, err := getFile(pid, s, b, id)
		if err != nil {
			return nil, nil, nil, err
		}
		defer f.Close()
		buf := make([]byte, 64*1024)
		for {
			select {
			case <-ctx.Done():
				return nil, nil, nil, ctx.Err()
			default:
			}
			n, err2 := reader.Read(buf)
			f.Write(buf[:n])
			if err2 == io.EOF {
				break
			}
		}
	} else if s.driver == "s3" {
		sess, err := session.NewSession(&aws.Config{
			Region:           aws.String(awsRegion),
			Endpoint:         aws.String(awsEndpoint),
			Credentials:      credentials.NewEnvCredentials(),
			S3ForcePathStyle: aws.Bool(awsPathstyle),
		})
		if err != nil {
			return nil, nil, b, err
		}
		uploader := s3manager.NewUploader(sess)
		_, err = uploader.UploadWithContext(ctx, &s3manager.UploadInput{
			Bucket: aws.String(s.bucket),
			Key:    aws.String(pid + "/" + s.filename),
			Body:   reader,
		})
		if err != nil {
			return nil, nil, nil, err
		}
	} else {
		return nil, nil, nil, fmt.Errorf("unsupported driver: %s", s.driver)
	}

	return hasher.Sum(nil), remoteHasher.Sum(nil), b, nil
}

type zipWriterCloser struct {
	writer    io.Writer
	zipWriter *zip.Writer
}

func (z zipWriterCloser) Write(p []byte) (n int, err error) {
	return z.writer.Write(p)
}

func (z zipWriterCloser) Close() error {
	err := z.zipWriter.Flush()
	if err != nil {
		return err
	}
	return z.zipWriter.Close()
}

func getFile(pid string, s storage, b *bytes.Buffer, id string) (io.WriteCloser, error) {
	if directUpload != "true" {
		zipWriter := zip.NewWriter(b)
		writer, err := zipWriter.Create(id)
		return zipWriterCloser{writer, zipWriter}, err
	}
	path := pathToFilesDir + pid + "/"
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		err := os.MkdirAll(path, os.ModePerm)
		if err != nil {
			return nil, err
		}
	}
	file := path + s.filename
	f, err := os.Create(file)
	if err != nil {
		return nil, err
	}
	return f, nil
}

func doHash(ctx context.Context, persistentId string, node tree.Node) ([]byte, error) {
	pid, err := trimProtocol(persistentId)
	if err != nil {
		return nil, err
	}
	storageIdentifier := node.Attributes.Metadata.DataFile.StorageIdentifier
	hashType := node.Attributes.RemoteHashType
	hasher, err := getHash(hashType, node.Attributes.Metadata.DataFile.Filesize)
	if err != nil {
		return nil, err
	}
	s := getStorage(storageIdentifier)
	var reader io.Reader
	if s.driver == "file" {
		file := pathToFilesDir + pid + "/" + s.filename
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
				Key:    aws.String(pid + "/" + s.filename),
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
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		_, err2 := r.Read(buf)
		if err2 == io.EOF {
			break
		}
	}
	return hasher.Sum(nil), nil
}

func trimProtocol(persistentId string) (string, error) {
	s := strings.Split(persistentId, ":")
	if len(s) < 2 {
		return "", fmt.Errorf("expected at least two parts of persistentId: protocol and remainder, found: %v", persistentId)
	}
	return strings.Join(s[1:], ":"), nil
}

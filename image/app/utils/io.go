// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package utils

import (
	"archive/zip"
	"context"
	"crypto/md5"
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"integration/app/plugin/types"
	"integration/app/tree"
	"io"
	"io/ioutil"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/google/uuid"
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
	if config.Options.DefaultDriver == "s3" {
		b = config.Options.S3Config.AWSBucket + ":"
	}
	return fmt.Sprintf("%s://%s%s", config.Options.DefaultDriver, b, fileName)
}

func getHash(hashType string, fileSize int) (hasher hash.Hash, err error) {
	if hashType == types.Md5 {
		hasher = md5.New()
	} else if hashType == types.SHA1 {
		hasher = sha1.New()
	} else if hashType == types.GitHash {
		hasher = sha1.New()
		hasher.Write([]byte(fmt.Sprintf("blob %d\x00", fileSize)))
	} else if hashType == types.FileSize {
		hasher = newFileSizeHash()
	} else {
		err = fmt.Errorf("unsupported hash type: %v", hashType)
	}
	return
}

func newFileSizeHash() hash.Hash {
	return &FileSizeHash{FileSize: 0}
}

type FileSizeHash struct {
	FileSize int
}

// Write (via the embedded io.Writer interface) adds more data to the running hash.
// It never returns an error.
func (h *FileSizeHash) Write(p []byte) (n int, err error) {
	h.FileSize = h.FileSize + len(p)
	return len(p), nil
}

// Sum appends the current hash to b and returns the resulting slice.
// It does not change the underlying hash state.
func (h *FileSizeHash) Sum(b []byte) []byte {
	res := make([]byte, 8)
	binary.LittleEndian.PutUint64(res, uint64(h.FileSize))
	return res
}

// Reset resets the Hash to its initial state.
func (h *FileSizeHash) Reset() {}

// Size returns the number of bytes Sum will return.
func (h *FileSizeHash) Size() int {
	return 8
}

// BlockSize returns the hash's underlying block size.
// The Write method must be able to accept any amount
// of data, but it may operate more efficiently if all writes
// are a multiple of the block size.
func (h *FileSizeHash) BlockSize() int {
	return 256
}

func write(ctx context.Context, dataverseKey string, fileStream types.Stream, storageIdentifier, persistentId, hashType, remoteHashType, id string, fileSize int) (hash []byte, remoteHash []byte, size int, retErr error) {
	pid, err := trimProtocol(persistentId)
	if err != nil {
		return nil, nil, 0, err
	}
	s := getStorage(storageIdentifier)
	hasher, err := getHash(hashType, fileSize)
	if err != nil {
		return nil, nil, 0, err
	}
	sizeHasher := &FileSizeHash{FileSize: 0}
	remoteHasher, err := getHash(remoteHashType, fileSize)
	if err != nil {
		return nil, nil, 0, err
	}
	readStream, err := fileStream.Open()
	defer fileStream.Close()
	if err != nil {
		return nil, nil, 0, err
	}
	reader := hashingReader{readStream, hasher}
	reader = hashingReader{reader, sizeHasher}
	reader = hashingReader{reader, remoteHasher}

	if s.driver == "file" || config.Options.DefaultDriver == "" || directUpload != "true" {
		wg := &sync.WaitGroup{}
		f, err1 := getFile(wg, dataverseKey, persistentId, pid, s, id)
		if err1 != nil {
			return nil, nil, 0, err1
		}
		defer f.Close()
		_, err = io.Copy(f, reader)
		wg.Wait()
		if err != nil {
			return nil, nil, 0, err
		}
	} else if s.driver == "s3" {
		sess, err := session.NewSession(&aws.Config{
			Region:           aws.String(config.Options.S3Config.AWSRegion),
			Endpoint:         aws.String(config.Options.S3Config.AWSEndpoint),
			Credentials:      credentials.NewEnvCredentials(),
			S3ForcePathStyle: aws.Bool(config.Options.S3Config.AWSPathstyle),
		})
		if err != nil {
			return nil, nil, 0, err
		}
		uploader := s3manager.NewUploader(sess)
		_, err = uploader.UploadWithContext(ctx, &s3manager.UploadInput{
			Bucket: aws.String(s.bucket),
			Key:    aws.String(pid + "/" + s.filename),
			Body:   reader,
		})
		if err != nil {
			return nil, nil, 0, err
		}
	} else {
		return nil, nil, 0, fmt.Errorf("unsupported driver: %s", s.driver)
	}

	return hasher.Sum(nil), remoteHasher.Sum(nil), sizeHasher.FileSize, nil
}

type zipWriterCloser struct {
	writer    io.Writer
	zipWriter *zip.Writer
	pw        io.WriteCloser
}

func (z zipWriterCloser) Write(p []byte) (n int, err error) {
	return z.writer.Write(p)
}

func (z zipWriterCloser) Close() error {
	err := z.zipWriter.Flush()
	if err != nil {
		return err
	}
	defer z.pw.Close()
	return z.zipWriter.Close()
}

func getFile(wg *sync.WaitGroup, dataverseKey, persistentId, pid string, s storage, id string) (io.WriteCloser, error) {
	if directUpload != "true" || config.Options.DefaultDriver == "" {
		pr, pw := io.Pipe()
		zipWriter := zip.NewWriter(pw)
		writer, err := zipWriter.Create(id)
		wg.Add(1)
		go swordAddFile(dataverseKey, persistentId, pr, wg, err)
		return zipWriterCloser{writer, zipWriter, pw}, err
	}
	path := config.Options.PathToFilesDir + pid + "/"
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

func doHash(ctx context.Context, dataverseKey, persistentId string, node tree.Node) ([]byte, error) {
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
	if config.Options.DefaultDriver == "" {
		readCloser, err := downloadFile(dataverseKey, node.Attributes.Metadata.DataFile.Id)
		if err != nil {
			return nil, err
		}
		defer readCloser.Close()
		reader = readCloser
	} else if s.driver == "file" {
		file := config.Options.PathToFilesDir + pid + "/" + s.filename
		f, err := os.Open(file)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		reader = f
	} else if s.driver == "s3" {
		sess, _ := session.NewSession(&aws.Config{
			Region:           aws.String(config.Options.S3Config.AWSRegion),
			Endpoint:         aws.String(config.Options.S3Config.AWSEndpoint),
			Credentials:      credentials.NewEnvCredentials(),
			S3ForcePathStyle: aws.Bool(config.Options.S3Config.AWSPathstyle),
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
	_, err = io.Copy(ioutil.Discard, r)
	return hasher.Sum(nil), err
}

func trimProtocol(persistentId string) (string, error) {
	s := strings.Split(persistentId, ":")
	if len(s) < 2 {
		return "", fmt.Errorf("expected at least two parts of persistentId: protocol and remainder, found: %v", persistentId)
	}
	return strings.Join(s[1:], ":"), nil
}

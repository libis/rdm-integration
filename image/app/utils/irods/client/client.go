package client

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/cyverse/go-irodsclient/fs"
	irods_fs "github.com/cyverse/go-irodsclient/irods/fs"
	"github.com/cyverse/go-irodsclient/irods/session"
	"github.com/cyverse/go-irodsclient/irods/types"
	"github.com/cyverse/go-irodsclient/irods/util"
)

// IrodsClient is used for interacting with irods.
type IrodsClient struct {
	Account    *types.IRODSAccount
	Session    *session.IRODSSession
	FileSystem *fs.FileSystem
}

type Domain struct {
	AuthScheme string
	Server     string
	Port       int
	Zone       string
}

var fileSystemConfig = &fs.FileSystemConfig{
	ApplicationName:       "iRODS_Go_RDR",
	CacheTimeout:          time.Minute,
	CacheCleanupTime:      time.Minute,
	ConnectionMax:         5,
	ConnectionIdleTimeout: 5 * time.Minute,
	OperationTimeout:      5 * time.Minute,
}

var sessionConfig = &session.IRODSSessionConfig{
	ApplicationName:       "iRODS_Go_RDR",
	ConnectionLifespan:    5 * time.Minute,
	OperationTimeout:      5 * time.Minute,
	ConnectionIdleTimeout: 5 * time.Minute,
	ConnectionMax:         5,
	ConnectionInitNumber:  5,
	ConnectionMaxIdle:     5,
	StartNewTransaction:   true,
}

var domainMap = map[string]Domain{
	"ghum test": {AuthScheme: "PAM", Server: "ghum.irods.icts.kuleuven.be", Port: 1247, Zone: "ghum"},
}

// NewIrodsClient creates a new IrodsClient.
func NewIrodsClient(domain, username, password string) (*IrodsClient, error) {
	d, ok := domainMap[domain]
	if !ok {
		return nil, fmt.Errorf("domain %s unknown", domain)
	}
	i := &IrodsClient{}

	var err error
	method, err := types.GetAuthScheme(d.AuthScheme)
	if err != nil {
		return nil, err
	}
	i.Account, err = types.CreateIRODSAccount(d.Server, d.Port, username, d.Zone, method, password, "")
	if err != nil {
		return nil, err
	}
	i.Account.CSNegotiationPolicy = "CS_NEG_REQUIRE"
	i.Account.ClientServerNegotiation = true

	i.Account.SSLConfiguration, err = types.CreateIRODSSSLConfig("/etc/ssl/certs/ca-bundle.crt", 32, "AES-256-CBC", 8, 16)
	if err != nil {
		return nil, err
	}

	i.Session, err = session.NewIRODSSession(i.Account, sessionConfig)
	if err != nil {
		return nil, err
	}

	i.FileSystem, err = fs.NewFileSystem(i.Account, fileSystemConfig)
	if err != nil {
		return nil, err
	}

	return i, nil
}

// Close an IrodsClient.
func (i *IrodsClient) Close() {
	i.Session.Release()
}

func (i *IrodsClient) GetDir(path string) ([]*fs.Entry, error) {
	// Add the zone name to the given path
	if !strings.HasPrefix(path, "/"+i.Account.ClientZone) {
		path = "/" + i.Account.ClientZone + path
	}
	path = util.GetCorrectIRODSPath(path)

	return i.FileSystem.List(path)
}

func (i *IrodsClient) StreamFile(irodsPath string) ([]byte, error) {
	if !strings.HasPrefix(irodsPath, "/"+i.Account.ClientZone) {
		irodsPath = "/" + i.Account.ClientZone + irodsPath
	}

	dir, err := i.GetDir(filepath.Dir(irodsPath))
	if err != nil {
		return nil, err
	}

	ok, fileSize := fileExistsAndAllowedSize(strings.TrimPrefix(irodsPath, "/"+i.Account.ClientZone), dir)

	if ok {
		conn, err := i.Session.AcquireConnection()
		if err != nil {
			return nil, err
		}
		defer i.Session.ReturnConnection(conn)

		handle, _, err := irods_fs.OpenDataObject(conn, irodsPath, "", "r")
		if err != nil {
			return nil, err
		}
		bytes := make([]byte, fileSize)
		irods_fs.ReadDataObject(conn, handle, bytes)
	}

	return nil, errors.New("file not found")
}

func fileExistsAndAllowedSize(file string, list []*fs.Entry) (bool, int64) {
	for _, b := range list {
		if b.Path == file && b.Size < 1500000000 {
			return true, b.Size
		}
	}
	return false, 0
}

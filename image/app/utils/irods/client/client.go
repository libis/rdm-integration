package client

import (
	"errors"
	"path/filepath"
	"strconv"
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

type Server struct {
	Server     string
	AuthScheme string
	Port       int
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

var serverMap = map[string]Server{
	"PAM://ghum.irods.icts.kuleuven.be:1247": {Server: "ghum.irods.icts.kuleuven.be", AuthScheme: "PAM", Port: 1247},
	"default":                                {Server: "ghum.irods.icts.kuleuven.be", AuthScheme: "PAM", Port: 1247},
}

// NewIrodsClient creates a new IrodsClient.
func NewIrodsClient(server, zone, username, password string) (*IrodsClient, error) {
	s := getServer(server)
	i := &IrodsClient{}

	var err error
	method, err := types.GetAuthScheme(s.AuthScheme)
	if err != nil {
		return nil, err
	}
	i.Account, err = types.CreateIRODSAccount(s.Server, s.Port, username, zone, method, password, "")
	if err != nil {
		return nil, err
	}
	i.Account.CSNegotiationPolicy = "CS_NEG_REQUIRE"
	i.Account.ClientServerNegotiation = true

	i.Account.SSLConfiguration, err = types.CreateIRODSSSLConfig("/etc/ssl/certs/ca-certificates.crt", 32, "AES-256-CBC", 8, 16)
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

func getServer(server string) Server {
	d, ok := serverMap[server]
	if !ok {
		d = serverMap["default"]
		if server != "" {
			s := strings.Split(server, "://")
			if len(s) > 1 {
				d.Server = s[1]
				if s[0] == string(types.AuthSchemeNative) || s[0] == string(types.AuthSchemeGSI) || s[0] == string(types.AuthSchemePAM) {
					d.AuthScheme = s[0]
				}
			} else {
				d.Server = s[0]
			}
			s = strings.Split(d.Server, "/")
			if len(s) > 1 {
				d.Server = s[0]
			}
			s = strings.Split(d.Server, ":")
			if len(s) > 1 {
				d.Server = s[0]
				port, err := strconv.Atoi(s[1])
				if err == nil {
					d.Port = port
				}
			}
		}
	}
	return d
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

	ok, fileSize := fileExistsAndAllowedSize(irodsPath, dir)

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
		_, err = irods_fs.ReadDataObject(conn, handle, bytes)
		return bytes, err
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

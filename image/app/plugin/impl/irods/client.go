// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package irods

import (
	"errors"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/cyverse/go-irodsclient/fs"
	"github.com/cyverse/go-irodsclient/irods/connection"
	irods_fs "github.com/cyverse/go-irodsclient/irods/fs"
	"github.com/cyverse/go-irodsclient/irods/session"
	"github.com/cyverse/go-irodsclient/irods/types"
)

type IrodsClient struct {
	Zone       string
	Session    *session.IRODSSession
	FileSystem *fs.FileSystem
}

type Server struct {
	Server     string
	AuthScheme string
	Port       int
}

type fileReader struct {
	handle  *types.IRODSFileHandle
	conn    *connection.IRODSConnection
	session *session.IRODSSession
}

var fileSystemConfig = &fs.FileSystemConfig{
	ApplicationName:       "iRODS_Go_RDR",
	CacheTimeout:          2 * time.Minute,
	CacheCleanupTime:      2 * time.Minute,
	ConnectionMax:         1,
	ConnectionIdleTimeout: 2 * time.Minute,
	OperationTimeout:      48 * time.Hour,
}

var sessionConfig = &session.IRODSSessionConfig{
	ApplicationName:       "iRODS_Go_RDR",
	ConnectionLifespan:    48 * time.Hour,
	OperationTimeout:      48 * time.Hour,
	ConnectionIdleTimeout: 2 * time.Minute,
	ConnectionMax:         1,
	ConnectionInitNumber:  1,
	ConnectionMaxIdle:     1,
	StartNewTransaction:   true,
}

var serverMap = map[string]Server{
	"PAM://ghum.irods.icts.kuleuven.be:1247": {Server: "ghum.irods.icts.kuleuven.be", AuthScheme: "PAM", Port: 1247},
	"default":                                {Server: "ghum.irods.icts.kuleuven.be", AuthScheme: "PAM", Port: 1247},
}

func NewIrodsClient(server, zone, username, password string) (*IrodsClient, error) {
	s := getServer(server)
	i := &IrodsClient{}
	i.Zone = zone

	var err error
	method, err := types.GetAuthScheme(s.AuthScheme)
	if err != nil {
		return nil, err
	}
	account, err := types.CreateIRODSAccount(s.Server, s.Port, username, zone, method, password, "")
	if err != nil {
		return nil, err
	}
	account.CSNegotiationPolicy = "CS_NEG_REQUIRE"
	account.ClientServerNegotiation = true

	account.SSLConfiguration, err = types.CreateIRODSSSLConfig("/etc/ssl/certs/ca-certificates.crt", 32, "AES-256-CBC", 8, 16)
	if err != nil {
		return nil, err
	}

	i.Session, err = session.NewIRODSSession(account, sessionConfig)
	if err != nil {
		return nil, err
	}

	i.FileSystem, err = fs.NewFileSystem(account, fileSystemConfig)
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

func (i *IrodsClient) Close() {
	i.FileSystem.Release()
	i.Session.Release()
}

func (i *IrodsClient) StreamFile(irodsPath string) (io.ReadCloser, error) {
	if i.FileSystem.ExistsFile(irodsPath) {
		conn, err := i.Session.AcquireConnection()
		if err != nil {
			return nil, err
		}
		handle, _, err := irods_fs.OpenDataObject(conn, irodsPath, "", "r")
		return &fileReader{handle, conn, i.Session}, err
	}
	return nil, errors.New("file not found")
}

func (fr *fileReader) Read(bytes []byte) (n int, err error) {
	n, err = irods_fs.ReadDataObject(fr.conn, fr.handle, bytes)
	return
}

func (fr *fileReader) Close() error {
	return fr.session.ReturnConnection(fr.conn)
}

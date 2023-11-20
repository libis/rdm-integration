// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package irods

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"integration/app/config"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/cyverse/go-irodsclient/fs"
	"github.com/cyverse/go-irodsclient/irods/session"
	"github.com/cyverse/go-irodsclient/irods/types"
)

const (
	ClientProgramName  string        = "iRODS_Go_RDR"
	connectionLifespan time.Duration = 168 * time.Hour
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

type Zone struct {
	Id                 string   `json:"jobid"`
	Fqdn               string   `json:"fqdn"`
	Zone               string   `json:"zone"`
	ParallelPortPrefix int      `json:"parallel_port_prefix"`
	Tag                string   `json:"tag"`
	ReRulesets         []string `json:"re_rulesets"`
	PythonRulesets     []string `json:"python_rulesets"`
	Paused             bool     `json:"paused"`
}

type ConnectionInfo struct {
	IrodsEnvironment `json:"irods_environment"`
	Password         string `json:"token"`
	Expiration       string `json:"expiration"`
}

type IrodsEnvironment struct {
	Host                    string `json:"irods_host"`
	Port                    int    `json:"irods_port"`
	Zone                    string `json:"irods_zone_name"`
	AuthenticationScheme    string `json:"irods_authentication_scheme"`
	EncryptionAlgorithm     string `json:"irods_encryption_algorithm"`
	EncryptionSaltSize      int    `json:"irods_encryption_salt_size"`
	EncryptionKeySize       int    `json:"irods_encryption_key_size"`
	EncryptionNumHashRounds int    `json:"irods_encryption_num_hash_rounds"`
	Username                string `json:"irods_user_name"`
	SslCaCertificateFile    string `json:"irods_ssl_ca_certificate_file"`
	SslVerifyServer         string `json:"irods_ssl_verify_server"`
	ClientServerNegotiation string `json:"irods_client_server_negotiation"`
	ClientServerPolicy      string `json:"irods_client_server_policy"`
	DefaultResource         string `json:"irods_default_resource"`
	Cwd                     string `json:"irods_cwd"`
}

var serverMap = map[string]Server{
	"PAM://ghum.irods.icts.kuleuven.be:1247": {Server: "ghum.irods.icts.kuleuven.be", AuthScheme: "PAM", Port: 1247},
	"default":                                {Server: "ghum.irods.icts.kuleuven.be", AuthScheme: "PAM", Port: 1247},
}

func NewIrodsClient(server, zone, username, password string) (*IrodsClient, error) {
	s := getServer(server)
	i := &IrodsClient{}
	i.Zone = zone
	var keySize = 32
	var saltSize = 8
	var hashRounds = 8
	var algorithm = "AES-256-CBC"
	var negotiationPolicy = types.CSNegotiationRequire("CS_NEG_REQUIRE")

	var err error
	if strings.Contains(server, "kuleuven") {
		info, err := getConnectionInfo(zone, password)
		if err != nil {
			return nil, err
		}
		s = Server{
			Server:     info.Host,
			AuthScheme: "PAM",
			Port:       info.Port,
		}
		username, password = info.Username, info.Password
		algorithm, keySize, saltSize, hashRounds = info.EncryptionAlgorithm, info.EncryptionKeySize, info.EncryptionSaltSize, info.EncryptionNumHashRounds
		negotiationPolicy = types.CSNegotiationRequire(info.ClientServerPolicy)
	}
	method, err := types.GetAuthScheme(s.AuthScheme)
	if err != nil {
		return nil, err
	}
	account, err := types.CreateIRODSAccount(s.Server, s.Port, username, zone, method, password, "")
	if err != nil {
		return nil, err
	}
	account.CSNegotiationPolicy = negotiationPolicy
	account.ClientServerNegotiation = true

	account.SSLConfiguration, err = types.CreateIRODSSSLConfig("/etc/ssl/certs/ca-certificates.crt", keySize, algorithm, saltSize, hashRounds)
	if err != nil {
		return nil, err
	}

	sessionConfig := session.NewIRODSSessionConfigWithDefault(ClientProgramName)
	i.Session, err = session.NewIRODSSession(account, sessionConfig)
	if err != nil {
		return nil, err
	}

	fsConfig := fs.NewFileSystemConfig(ClientProgramName, fs.FileSystemConnectionErrorTimeoutDefault, fs.FileSystemConnectionInitNumberDefault, connectionLifespan,
		connectionLifespan, fs.FileSystemTimeoutDefault, config.GetNbSubTasks()+2, fs.FileSystemTCPBufferSizeDefault,
		fs.FileSystemTimeoutDefault, fs.FileSystemTimeoutDefault, []fs.MetadataCacheTimeoutSetting{}, true, true)
	i.FileSystem, err = fs.NewFileSystem(account, fsConfig)
	if err != nil {
		i.Session.Release()
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

type fileReader struct {
	handle *fs.FileHandle
	buffer []byte
	i      int
	n      int
	err    error
}

func (i *IrodsClient) StreamFile(irodsPath string) (io.ReadCloser, error) {
	if i.FileSystem.ExistsFile(irodsPath) {
		handle, err := i.FileSystem.OpenFile(irodsPath, "", "r")
		return &fileReader{handle, make([]byte, fs.FileSystemTCPBufferSizeDefault), 0, 0, nil}, err
	}
	return nil, errors.New("file not found")
}

func (fr *fileReader) Read(bytes []byte) (int, error) {
	if fr.n == 0 {
		fr.n, fr.err = fr.handle.Read(fr.buffer)
		if fr.err != nil && fr.err != io.EOF {
			return 0, fr.err
		}
	}
	toCopy := fr.n - fr.i
	if toCopy > len(bytes) {
		toCopy = len(bytes)
	}
	n := copy(bytes, fr.buffer[fr.i:fr.i+toCopy])
	fr.i = fr.i + n
	if fr.i == fr.n {
		fr.i = 0
		fr.n = 0
		return n, fr.err
	}
	return n, nil
}

func (fr *fileReader) Close() error {
	return fr.handle.Close()
}

func getConnectionInfo(zone, token string) (ConnectionInfo, error) {
	zoneId, err := getZoneId(zone, token)
	if err != nil {
		return ConnectionInfo{}, err
	}
	url := "https://icts-p-coz-data-platform-api.cloud.icts.kuleuven.be/v1/irods/zones/" + zoneId + "/connection_info"
	shortContext, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	res := ConnectionInfo{}
	request, _ := http.NewRequestWithContext(shortContext, "GET", url, nil)
	request.Header.Add("accept", "application/json")
	request.Header.Add("Authorization", "Bearer "+token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return ConnectionInfo{}, err
	}
	defer response.Body.Close()
	responseData, _ := io.ReadAll(response.Body)
	err = json.Unmarshal(responseData, &res)
	return res, err
}

func getZoneId(zone, token string) (string, error) {
	zones, err := getZones(token)
	if err != nil {
		return "", err
	}
	for _, z := range zones {
		if z.Zone == zone {
			return z.Id, nil
		}
	}
	return "", fmt.Errorf("zone %s not found", zone)
}

func getZones(token string) ([]Zone, error) {
	url := "https://icts-p-coz-data-platform-api.cloud.icts.kuleuven.be/v1/irods/zones"
	shortContext, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	res := []Zone{}
	request, _ := http.NewRequestWithContext(shortContext, "GET", url, nil)
	request.Header.Add("accept", "application/json")
	request.Header.Add("Authorization", "Bearer "+token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	responseData, _ := io.ReadAll(response.Body)
	err = json.Unmarshal(responseData, &res)
	return res, err
}

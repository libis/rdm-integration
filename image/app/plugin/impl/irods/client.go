// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package irods

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	plugintypes "integration/app/plugin/types"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/cyverse/go-irodsclient/fs"
	"github.com/cyverse/go-irodsclient/irods/common"
	"github.com/cyverse/go-irodsclient/irods/connection"
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
	"native://ghum.irods.icts.kuleuven.be:1247": {Server: "ghum.irods.icts.kuleuven.be", AuthScheme: "native", Port: 1247},
	"default": {Server: "ghum.irods.icts.kuleuven.be", AuthScheme: "native", Port: 1247},
}

func NewIrodsClient(server, zone, username, password string) (*IrodsClient, error) {
	s := getServer(server)
	i := &IrodsClient{}
	i.Zone = zone
	var keySize = 32
	var saltSize = 8
	var hashRounds = 8
	var algorithm = "AES-256-CBC"
	var negotiationPolicy = types.CSNegotiationPolicyRequestSSL

	var err error
	if strings.Contains(server, "kuleuven") {
		info, err := getConnectionInfo(zone, password)
		if err != nil {
			return nil, fmt.Errorf("iRODS connection information could not be retrieved")
		}
		s = Server{
			Server:     info.Host,
			AuthScheme: info.AuthenticationScheme,
			Port:       info.Port,
		}
		username, password = info.Username, info.Password
		algorithm, keySize, saltSize, hashRounds = info.EncryptionAlgorithm, info.EncryptionKeySize, info.EncryptionSaltSize, info.EncryptionNumHashRounds
		negotiationPolicy = types.GetCSNegotiationPolicyRequest(info.ClientServerPolicy)
	}
	method := types.GetAuthScheme(strings.ToLower(s.AuthScheme))
	account, err := types.CreateIRODSAccount(s.Server, s.Port, username, zone, method, password, "")
	if err != nil {
		return nil, fmt.Errorf("iRODS account could not be initialized")
	}
	account.CSNegotiationPolicy = negotiationPolicy
	account.ClientServerNegotiation = true

	account.SSLConfiguration = &types.IRODSSSLConfig{
		CACertificateFile:       "/etc/ssl/certs/ca-certificates.crt",
		CACertificatePath:       "",
		EncryptionKeySize:       keySize,
		EncryptionAlgorithm:     algorithm,
		EncryptionSaltSize:      saltSize,
		EncryptionNumHashRounds: hashRounds,
	}

	if account.AuthenticationScheme == types.AuthSchemePAM {
		// Make a single connection using PAM to retrieve a "native" password with a longer lifetime
		account.PamTTL = 168 // hours

		conn, err := connection.NewIRODSConnection(account, nil)
		if err != nil {
			return nil, fmt.Errorf("iRODS connection could not be initialized")
		}
		err = conn.Connect()
		if err != nil {
			conn.Disconnect()
			return nil, fmt.Errorf("iRODS connection failed")
		}
		nativePass := conn.GetAccount().PAMToken
		conn.Disconnect()

		// Future connections use native protocol
		account.Password = nativePass
		account.AuthenticationScheme = types.AuthSchemeNative
	}

	i.Session, err = session.NewIRODSSession(account, nil)
	if err != nil {
		return nil, fmt.Errorf("iRODS session could not be initialized")
	}

	fsConfig := fs.NewFileSystemConfig(ClientProgramName)
	fsConfig.IOConnection.Lifespan = types.Duration(connectionLifespan)
	fsConfig.IOConnection.OperationTimeout = types.Duration(connectionLifespan)
	i.FileSystem, err = fs.NewFileSystem(account, fsConfig)
	if err != nil {
		i.Session.Release()
		return nil, fmt.Errorf("iRODS filesystem could not be initialized")
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
				d.AuthScheme = s[0]
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

func (i *IrodsClient) Close() error {
	i.FileSystem.Release()
	i.Session.Release()
	return nil
}

func (i *IrodsClient) StreamFile(irodsPath string) (io.ReadCloser, error) {
	if i.FileSystem.ExistsFile(irodsPath) {
		reader, err := i.FileSystem.OpenFile(irodsPath, "", "r")
		return reader, classifyOpenError(err)
	}
	return nil, plugintypes.NewUnrecoverableError(errors.New("file not found"))
}

// classifyOpenError marks open failures that retrying within one job cannot
// heal as unrecoverable, so jobs fail fast with an actionable message instead
// of burning the whole retry budget. HIERARCHY_ERROR means no replica can be
// opened — typically the object is write-locked by an upload that is still
// running (or that died without closing). Waiting it out is exactly the
// read-during-write hazard, so the user should retry after the upload is done.
func classifyOpenError(err error) error {
	if err == nil {
		return nil
	}
	if types.GetIRODSErrorCode(err) == common.HIERARCHY_ERROR {
		return plugintypes.NewUnrecoverableError(fmt.Errorf(
			"file is locked, possibly by an upload that is still in progress or was interrupted; retry after the upload completes"))
	}
	if types.IsFileNotFoundError(err) {
		return plugintypes.NewUnrecoverableError(errors.New("file not found"))
	}
	return fmt.Errorf("iRODS file open failed")
}

func getConnectionInfo(zone, token string) (ConnectionInfo, error) {
	zoneId, err := getZoneId(zone, token)
	if err != nil {
		return ConnectionInfo{}, err
	}
	url := "https://icts-p-coz-data-platform-api.cloud.icts.kuleuven.be/v2/kuleuven/irods/zone/" + zoneId + "/connection-info"
	shortContext, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	res := ConnectionInfo{}
	request, _ := http.NewRequestWithContext(shortContext, "POST", url, nil)
	request.Header.Add("accept", "application/json")
	request.Header.Add("Authorization", "Bearer "+token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return ConnectionInfo{}, fmt.Errorf("iRODS connection information request failed")
	}
	defer response.Body.Close()
	responseData, _ := io.ReadAll(response.Body)
	err = json.Unmarshal(responseData, &res)
	if err != nil {
		return ConnectionInfo{}, fmt.Errorf("iRODS connection information response could not be parsed")
	}
	return res, nil
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
	return "", fmt.Errorf("iRODS zone was not found")
}

func getZones(token string) ([]Zone, error) {
	url := "https://icts-p-coz-data-platform-api.cloud.icts.kuleuven.be/v2/kuleuven/irods/zones"
	shortContext, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	res := []Zone{}
	request, _ := http.NewRequestWithContext(shortContext, "GET", url, nil)
	request.Header.Add("accept", "application/json")
	request.Header.Add("Authorization", "Bearer "+token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("iRODS zone request failed")
	}
	defer response.Body.Close()
	responseData, _ := io.ReadAll(response.Body)
	err = json.Unmarshal(responseData, &res)
	if err != nil {
		return nil, fmt.Errorf("iRODS zone response could not be parsed")
	}
	return res, nil
}

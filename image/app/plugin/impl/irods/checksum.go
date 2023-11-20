// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package irods

import (
	"fmt"
	"integration/app/plugin/types"
	"strings"

	"github.com/cyverse/go-irodsclient/irods/fs"
)

func (i *IrodsClient) Checksum(irodsPath string) (string, string, error) {
	conn, err := i.Session.AcquireConnection()
	if err != nil {
		return "", "", err
	}
	defer i.Session.ReturnConnection(conn)
	cs, err := fs.GetDataObjectChecksum(conn, irodsPath, "")
	if err != nil {
		return "", "", err
	}
	hashType := strings.ToUpper(string(cs.Algorithm))
	if hashType == "SHA-256" {
		hashType = types.SHA256
	}
	if hashType == "SHA-512" {
		hashType = types.SHA512
	}
	if hashType != types.Md5 && hashType != types.SHA256 && hashType != types.SHA512 {
		return "", "", fmt.Errorf("unknown hash type: %v", hashType)
	}
	return hashType, fmt.Sprintf("%x", cs.Checksum), err
}

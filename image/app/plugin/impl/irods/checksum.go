// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package irods

import (
	"fmt"
	"integration/app/plugin/types"
	"strings"

	"github.com/cyverse/go-irodsclient/irods/fs"
)

func (i *IrodsClient) Checksum(irodsPath string) (string, string, error) {
	conn, err := i.Session.AcquireConnection(false)
	if err != nil {
		return "", "", err
	}
	defer i.Session.ReturnConnection(conn)
	cs, err := fs.GetDataObjectChecksum(conn, irodsPath, "")
	if err != nil {
		return "", "", err
	}
	hashType, err := normalizeHashType(string(cs.Algorithm))
	if err != nil {
		return "", "", err
	}
	return hashType, fmt.Sprintf("%x", cs.Checksum), nil
}

func normalizeHashType(algorithm string) (string, error) {
	hashType := strings.ToUpper(algorithm)
	if hashType == "SHA-256" {
		hashType = types.SHA256
	}
	if hashType == "SHA-512" {
		hashType = types.SHA512
	}
	if hashType != types.Md5 && hashType != types.SHA256 && hashType != types.SHA512 {
		return "", fmt.Errorf("unknown hash type: %v", algorithm)
	}
	return hashType, nil
}

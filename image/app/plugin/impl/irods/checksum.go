// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package irods

import (
	"encoding/xml"
	"fmt"

	"github.com/cyverse/go-irodsclient/irods/common"
	"github.com/cyverse/go-irodsclient/irods/connection"
	"github.com/cyverse/go-irodsclient/irods/message"
	"github.com/cyverse/go-irodsclient/irods/types"
)

func (i *IrodsClient) Checksum(irodsPath string) (string, error) {
	conn, err := i.Session.AcquireConnection()
	if err != nil {
		return "", err
	}
	defer i.Session.ReturnConnection(conn)
	return checksum(conn, irodsPath)
}

type ChecksumRequest message.IRODSMessageDataObjectRequest

func (msg *ChecksumRequest) GetMessage() (*message.IRODSMessage, error) {
	bytes, err := xml.Marshal(msg)
	if err != nil {
		return nil, err
	}

	msgBody := message.IRODSMessageBody{
		Type:    message.RODS_MESSAGE_API_REQ_TYPE,
		Message: bytes,
		Error:   nil,
		Bs:      nil,
		IntInfo: int32(common.DATA_OBJ_CHKSUM_AN),
	}

	msgHeader, err := msgBody.BuildHeader()
	if err != nil {
		return nil, err
	}

	return &message.IRODSMessage{
		Header: msgHeader,
		Body:   &msgBody,
	}, nil
}

// A Response to retrieve from irods.
type ChecksumResponse struct {
	Checksum string
}

type STRI_PI struct {
	MyStr string `xml:"myStr"`
}

func (c *ChecksumResponse) FromMessage(m *message.IRODSMessage) error {
	if m == nil || m.Body == nil {
		return fmt.Errorf("response message has no body")
	}
	res := STRI_PI{}
	err := xml.Unmarshal(m.Body.Message, &res)
	c.Checksum = res.MyStr
	return err
}

func (c *ChecksumResponse) CheckError() error {
	if len(c.Checksum) == 0 {
		return fmt.Errorf("checksum not present in response message")
	}
	return nil
}

func checksum(conn *connection.IRODSConnection, irodsPath string) (string, error) {
	metrics := conn.GetMetrics()
	if metrics != nil {
		metrics.IncreaseCounterForDataObjectOpen(1)
	}
	conn.Lock()
	defer conn.Unlock()

	fileOpenMode := types.FileOpenMode("r")
	resource := conn.GetAccount().DefaultResource
	flag := fileOpenMode.GetFlag()
	request := &ChecksumRequest{
		Path:          irodsPath,
		CreateMode:    0,
		OpenFlags:     flag,
		Offset:        0,
		Size:          -1,
		Threads:       0,
		OperationType: 0,
		KeyVals:       message.IRODSMessageSSKeyVal{},
	}

	if len(resource) > 0 {
		request.KeyVals.Add(string(common.DEST_RESC_NAME_KW), resource)
	}

	response := ChecksumResponse{}

	err := conn.RequestAndCheck(request, &response, nil)
	if err != nil {
		if types.GetIRODSErrorCode(err) == common.CAT_NO_ROWS_FOUND {
			return "", types.NewFileNotFoundErrorf("could not find a data object")
		}

		return "", err
	}

	return response.Checksum, nil
}

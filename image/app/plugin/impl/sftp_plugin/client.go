package sftp_plugin

import (
	"fmt"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

type client struct {
	SftpClient *sftp.Client
	Close      func() error
}

func getClient(sftpUrl, user, pass string) (*client, error) {
	auths := []ssh.AuthMethod{ssh.Password(pass)}
	config := ssh.ClientConfig{
		User:            user,
		Auth:            auths,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	conn, err := ssh.Dial("tcp", sftpUrl, &config)
	if err != nil {
		return nil, fmt.Errorf("SFTP connection failed")
	}
	cl, err := sftp.NewClient(conn)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("unable to start SFTP subsystem")
	}

	return &client{
		SftpClient: cl,
		Close: func() error {
			if err := cl.Close(); err != nil {
				conn.Close()
				return fmt.Errorf("SFTP client close failed")
			}
			conn.Close()
			return nil
		}}, nil
}

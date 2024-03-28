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
		return nil, fmt.Errorf("failed to connecto to [%s]: %v", sftpUrl, err)
	}
	cl, err := sftp.NewClient(conn)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("unable to start SFTP subsystem: %v", err)
	}

	return &client{
		SftpClient: cl,
		Close: func() error {
			err := cl.Close()
			conn.Close()
			return err
		}}, nil
}

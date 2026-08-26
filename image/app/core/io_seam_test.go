// Author: Eryk Kulikowski @ KU Leuven (2026). Apache 2.0 License

package core

import (
	"context"
	"integration/app/plugin/types"
	"io"
	"strings"
	"sync"
	"testing"
)

type discardWriteCloser struct{}

func (discardWriteCloser) Write(p []byte) (int, error) { return len(p), nil }
func (discardWriteCloser) Close() error                { return nil }

// Seam test for the truncation guard inside write(): a source stream that
// ends early without an error (an iRODS short read surfaces as EOF) must fail
// the transfer, not store and register a truncated file as a success. The
// unit tests on checkTransferredSize keep passing even if the call to it is
// dropped from write() — this exercises the real streaming path.
func TestWriteFailsOnTruncatedSourceStream(t *testing.T) {
	savedDestination := Destination
	defer func() { Destination = savedDestination }()
	Destination = DestinationPlugin{
		IsDirectUpload: func() bool { return false },
		WriteOverWire: func(ctx context.Context, dbId int64, nodeMapId, mimeType, token, user, persistentId string, wg *sync.WaitGroup, async_err *ErrorHolder) (io.WriteCloser, error) {
			return discardWriteCloser{}, nil
		},
	}

	content := "only 21 bytes arrived"
	stream := types.Stream{
		Open:  func() (io.Reader, error) { return strings.NewReader(content), nil },
		Close: func() error { return nil },
	}

	reportedSize := int64(1000) // the source claimed more than the stream delivers
	_, _, _, err := write(context.Background(), 0, "dv-key", "user", stream,
		"file://truncated.bin", "doi:10.1/TEST", "MD5", "MD5", "truncated.bin", "application/octet-stream", reportedSize)

	if err == nil {
		t.Fatal("expected the truncated transfer to fail, got success")
	}
	if !strings.Contains(err.Error(), "incomplete transfer") {
		t.Errorf("expected an incomplete-transfer error, got: %v", err)
	}

	// The same stream with a truthful size must pass.
	_, _, size, err := write(context.Background(), 0, "dv-key", "user", stream,
		"file://ok.bin", "doi:10.1/TEST", "MD5", "MD5", "ok.bin", "application/octet-stream", int64(len(content)))
	if err != nil {
		t.Fatalf("expected the complete transfer to succeed, got: %v", err)
	}
	if size != int64(len(content)) {
		t.Errorf("expected the counted size %v, got %v", len(content), size)
	}
}

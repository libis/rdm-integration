package types

import "io"

type Stream struct {
	Open  func() (io.Reader, error)
	Close func() error
}

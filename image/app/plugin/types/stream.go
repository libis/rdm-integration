// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package types

import "io"

type Stream struct {
	Open  func() (io.Reader, error)
	Close func() error
}

type StreamsType struct {
	Streams map[string]Stream
	Cleanup func() error
}

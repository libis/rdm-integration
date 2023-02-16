// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package core

import (
	"hash"
	"io"
	"mime/multipart"
)

type storage struct {
	driver   string
	bucket   string
	filename string
}

type hashingReader struct {
	reader io.Reader
	hasher hash.Hash
}

type ErrorHolder struct {
	Err error
}

type writerCloser struct {
	writer io.Writer
	closer io.Closer
	pw     io.WriteCloser
}

func (z writerCloser) Write(p []byte) (n int, err error) {
	return z.writer.Write(p)
}

func (z writerCloser) Close() error {
	defer z.pw.Close()
	return z.closer.Close()
}

type fileWriter struct {
	part1writtern bool
	part1bytes    []byte
	part2         io.Writer
	writer        *multipart.Writer
	filename      string
}

func newFileWriter(filename string, part1bytes []byte, writer *multipart.Writer) *fileWriter {
	return &fileWriter{false, part1bytes, nil, writer, filename}
}

func (f *fileWriter) Write(p []byte) (int, error) {
	if !f.part1writtern {
		part1, _ := f.writer.CreateFormField("jsonData")
		part1.Write(f.part1bytes)
		f.part1writtern = true
		f.part2, _ = f.writer.CreateFormFile("file", f.filename)
	}
	n, err := f.part2.Write(p)
	return n, err
}

func (f *fileWriter) Close() error {
	if !f.part1writtern {
		f.Write([]byte{})
	}
	return f.writer.Close()
}

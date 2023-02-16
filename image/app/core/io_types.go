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

type WriterCloser struct {
	writer io.Writer
	closer io.Closer
	pw     io.WriteCloser
}

func NewWritterCloser(writer io.Writer, closer io.Closer, pipeWriter io.WriteCloser) WriterCloser {
	return WriterCloser{writer, closer, pipeWriter}
}

func (z WriterCloser) Write(p []byte) (n int, err error) {
	return z.writer.Write(p)
}

func (z WriterCloser) Close() error {
	defer z.pw.Close()
	return z.closer.Close()
}

type FileWriter struct {
	part1writtern bool
	part1bytes    []byte
	part2         io.Writer
	writer        *multipart.Writer
	filename      string
}

func NewFileWriter(filename string, part1bytes []byte, writer *multipart.Writer) *FileWriter {
	return &FileWriter{false, part1bytes, nil, writer, filename}
}

func (f *FileWriter) Write(p []byte) (int, error) {
	if !f.part1writtern {
		part1, _ := f.writer.CreateFormField("jsonData")
		part1.Write(f.part1bytes)
		f.part1writtern = true
		f.part2, _ = f.writer.CreateFormFile("file", f.filename)
	}
	n, err := f.part2.Write(p)
	return n, err
}

func (f *FileWriter) Close() error {
	if !f.part1writtern {
		f.Write([]byte{})
	}
	return f.writer.Close()
}

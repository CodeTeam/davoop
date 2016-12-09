package davoop

import (
	"os"

	"github.com/colinmarc/hdfs"
)

type File interface {
	Stat() (os.FileInfo, error)
	Readdir(count int) ([]os.FileInfo, error)

	Read([]byte) (int, error)
	Write(p []byte) (n int, err error)
	Seek(offset int64, whence int) (int64, error)
	Close() error

	/* TODO: needed?
		Chdir() error
	    Chmod(mode FileMode) error
	    Chown(uid, gid int) error
	*/
}

type HDFSFile struct {
	hdfs *hdfs.FileReader
	fw   *hdfs.FileWriter
}

func (f HDFSFile) Stat() (os.FileInfo, error) {
	return f.hdfs.Stat(), nil
}

func (f HDFSFile) Readdir(count int) ([]os.FileInfo, error) {
	return f.hdfs.Readdir(count)
}

func (f HDFSFile) Read(b []byte) (int, error) {
	return f.hdfs.Read(b)
}

func (f HDFSFile) Write(p []byte) (n int, err error) {
	return f.fw.Write(p)
}

func (f HDFSFile) Seek(offset int64, whence int) (int64, error) {
	return f.hdfs.Seek(offset, whence)
}

func (f HDFSFile) Close() error {
	if f.hdfs != nil {
		if err := f.hdfs.Close(); err != nil {
			return err
		}
	}

	if f.fw != nil {
		if err := f.fw.Close(); err != nil {
			return err
		}
	}

	return nil
}

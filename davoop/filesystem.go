package davoop

import (
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/codeteam/davoop/webdav"
	"github.com/colinmarc/hdfs"
)

type HDFSDir struct {
	startPath string
	hdfs      *hdfs.Client
}

func NewHDFSDir(address string, user string, startPath string) (HDFSDir, error) {
	fs := HDFSDir{
		startPath: startPath,
	}

	if fshd, err := hdfs.NewForUser(address, user); err != nil {
		return fs, err
	} else {
		fs.hdfs = fshd
		return fs, nil
	}
}

func (d HDFSDir) sanitizePath(name string) (string, error) {
	if filepath.Separator != '/' && strings.IndexRune(name, filepath.Separator) >= 0 ||
		strings.Contains(name, "\x00") {
		return "", webdav.ErrInvalidCharPath
	}

	dir := d.startPath
	if dir == "" {
		dir = "."
	}
	dir = filepath.Join(dir, filepath.FromSlash(path.Clean("/"+name)))

	log.Println(name, " -> ", dir)

	return dir, nil
}

func (d HDFSDir) Open(name string) (webdav.File, error) {
	p, err := d.sanitizePath(name)
	if err != nil {
		return nil, err
	}

	f, err := d.hdfs.Open(p)
	if err != nil {
		return nil, err
	}
	return HDFSFile{hdfs: f}, nil
}

func (d HDFSDir) Create(name string) (webdav.File, error) {
	p, err := d.sanitizePath(name)
	if err != nil {
		return nil, err
	}

	f, err := d.hdfs.Create(p)
	if err != nil {
		return nil, err
	}

	if fr, err := d.hdfs.Open(p); err != nil {
		return nil, err
	} else {
		return HDFSFile{hdfs: fr, fw: f}, nil
	}
}

// Mkdir creates a new directory with the specified name
func (d HDFSDir) Mkdir(name string) error {
	p, err := d.sanitizePath(name)
	if err != nil {
		return err
	}

	return d.hdfs.Mkdir(p, os.ModePerm)
}

func (d HDFSDir) Remove(name string) error {
	p, err := d.sanitizePath(name)
	if err != nil {
		return err
	}

	return d.hdfs.Remove(p)
}

package webdav

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"mime"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func ExecDir() (string, error) {
	file, err := exec.LookPath(os.Args[0])
	if err != nil {
		return "", err
	}
	p, err := filepath.Abs(file)
	if err != nil {
		return "", err
	}
	return path.Dir(strings.Replace(p, "\\", "/", -1)), nil
}

func init() {
	// exePath, err := ExecDir()
	// if err != nil {
	// 	panic(err)
	// }
}

func Handler(root FileSystem) http.Handler {
	return &Server{Fs: root}
}

type Server struct {
	// trimmed path prefix
	TrimPrefix string

	// files are readonly?
	ReadOnly bool

	// generate directory listings?
	Listings bool

	// access to a collection of named files
	Fs FileSystem

	tokens_to_lock map[string]*Lock

	path_to_token map[string]string
}

func generateToken() string {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	return fmt.Sprintf("%d-%d-00105A989226:%d",
		r.Int31(), r.Int31(), time.Now().UnixNano())
}

func NewServer(dir, prefix string, listDir bool) *Server {
	d := Dir(dir)
	return &Server{
		Fs:             d,
		TrimPrefix:     prefix,
		Listings:       listDir,
		tokens_to_lock: make(map[string]*Lock),
		path_to_token:  make(map[string]string),
	}
}

var (
	PullMethods = map[string]bool{
		"OPTIONS":  true,
		"GET":      true,
		"HEAD":     true,
		"PROPFIND": true}

	PushMethods = map[string]bool{
		"POST":      true,
		"DELETE":    true,
		"PUT":       true,
		"PROPPATCH": true,
		"MKCOL":     true,
		"COPY":      true,
		"MOVE":      true,
		"LOCK":      true,
		"UNLOCK":    true,
	}
)

func IsPullMethod(method string) bool {
	_, ok := PullMethods[method]
	return ok
}

func IsPushMethod(method string) bool {
	_, ok := PushMethods[method]
	return ok
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	//log.Println("DAV:", r.RemoteAddr, r.Method, r.URL)

	switch r.Method {
	case "OPTIONS":
		s.doOptions(w, r)

	case "GET":
		s.doGet(w, r)
	case "HEAD":
		s.doHead(w, r)
	case "POST":
		s.doPost(w, r)
	case "DELETE":
		s.doDelete(w, r)
	case "PUT":
		s.doPut(w, r)

	case "PROPFIND":
		s.doPropfind(w, r)
	case "PROPPATCH":
		s.doProppatch(w, r)
	case "MKCOL":
		s.doMkcol(w, r)
	case "COPY":
		s.doCopy(w, r)
	case "MOVE":
		s.doMove(w, r)

	case "LOCK":
		s.doLock(w, r)
	case "UNLOCK":
		s.doUnlock(w, r)

	default:
		// qlog.Error("DAV:", "unknown method", r.Method)
		w.WriteHeader(StatusBadRequest)
	}
}

func (s *Server) methodsAllowed(path string) string {
	if !s.pathExists(path) {
		return "OPTIONS, MKCOL, PUT, LOCK"
	}

	allowed := "OPTIONS, GET, HEAD, POST, DELETE, TRACE, PROPPATCH, COPY, MOVE, LOCK, UNLOCK"

	if s.Listings {
		allowed += ", PROPFIND"
	}

	if s.pathIsDirectory(path) {
		allowed += ", PUT"
	}

	return allowed
}

// convert request url to path
func (s *Server) url2path(u *url.URL) string {
	if u.Path == "" {
		return "/"
	}

	if p := strings.TrimPrefix(u.Path, s.TrimPrefix); len(p) < len(u.Path) {
		return strings.Trim(p, "/")
	}

	return "/"
}

// convert path to url
func (s *Server) path2url(p string) *url.URL {
	return &url.URL{Path: path.Join("/", s.TrimPrefix, p)}
}

// does path exists?
func (s *Server) pathExists(path string) bool {
	f, err := s.Fs.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	return true
}

// is path a directory?
func (s *Server) pathIsDirectory(path string) bool {
	f, err := s.Fs.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return false
	}

	return fi.IsDir()
}

func (s *Server) directoryContents(path string) []string {
	f, err := s.Fs.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	fi, err := f.Readdir(0)
	if err != nil {
		return nil
	}

	ret := make([]string, len(fi))
	for k, i := range fi {
		name := i.Name()
		if i.IsDir() {
			name += "/"
		}
		ret[k] = name
	}

	return ret
}

// is path in request locked?
func (s *Server) isLockedRequest(r *http.Request) bool {
	return s.isLocked(
		s.url2path(r.URL),
		r.Header.Get("If") /*+r.Header.Get("Lock-Token")*/)
}

// is path locked?
func (s *Server) isLocked(path, ifHeader string) bool {
	token, ok := s.path_to_token[path]
	if !ok {
		return false
	}

	if ifHeader == "" {
		return true
	}

	taglist := IfParser(ifHeader)
	found := false
	for _, tag := range taglist {
		for _, listitem := range tag.list {
			token = tokenFinder(listitem)
			if (token != "") &&
				s.hasLock(token) &&
				(s.getLock(token).uri == path) {
				found = true
				break
			}
		}
		if found {
			break
		}
	}

	return !found
}

func (s *Server) hasLock(token string) bool {
	_, ok := s.tokens_to_lock[token]
	return ok
}

func (s *Server) getToken(uri string) string {
	return s.path_to_token[uri]
}

func (s *Server) getLock(token string) *Lock {
	return s.tokens_to_lock[token]
}

func (s *Server) delLock(token string) {
	if lock, ok := s.tokens_to_lock[token]; ok {
		delete(s.path_to_token, lock.uri)
		delete(s.tokens_to_lock, token)
	}
}

func (s *Server) setLock(lock *Lock) {
	s.tokens_to_lock[lock.token] = lock
	s.path_to_token[lock.uri] = lock.token
}

func (s *Server) lockResource(path string) {
	// TODO
}

func (s *Server) unlockResource(path string) {
	// TODO
}

// The PROPFIND method retrieves properties defined on the resource identified by the Request-URI
// http://www.webdav.org/specs/rfc4918.html#METHOD_PROPFIND
func (s *Server) doPropfind(w http.ResponseWriter, r *http.Request) {
	if !s.Listings {
		w.Header().Set("Allow", s.methodsAllowed(s.url2path(r.URL)))
		w.WriteHeader(StatusMethodNotAllowed)
		return
	}

	depth := r.Header.Get("Depth")
	switch depth {
	case "0", "1":
	case "", "infinity":
		// treat as infinity if no depth header was included
		// disable infinity for performance and security concerns
		// http://www.webdav.org/specs/rfc4918.html#rfc.section.9.1.1
		w.WriteHeader(StatusForbidden)
		return
	default:
		w.WriteHeader(StatusBadRequest)
		return
	}

	var propnames bool
	var properties []string
	var includes []string

	if r.ContentLength > 0 {
		propfind, err := NodeFromXml(r.Body)
		if err != nil {
			w.WriteHeader(StatusBadRequest)
			return
		}

		if propfind.Name.Local != "propfind" {
			w.WriteHeader(StatusBadRequest)
			return
		}

		// find by property
		// http://www.webdav.org/specs/rfc4918.html#dav.properties
		if propfind.HasChildren("prop") {
			prop := propfind.FirstChildren("prop")
			for _, p := range prop.GetChildrens("*") {
				properties = append(properties, p.Name.Local)
			}
		}

		// find property names
		if propfind.HasChildren("propname") {
			propnames = true
		}

		// find all properties
		if propfind.HasChildren("allprop") {
			properties = []string{
				"creationdate", "displayname",
				"getcontentlanguage", "getcontentlength",
				"getcontenttype", "getetag",
				"getlastmodified", "lockdiscovery",
				"resourcetype", "supportedlock",
			}

			if propfind.HasChildren("include") {
				for _, i := range propfind.GetChildrens("include") {
					includes = append(includes, i.Name.Local)
				}
			}
		}
	}

	path := s.url2path(r.URL)
	if !s.pathExists(path) {
		http.Error(w, path, StatusNotFound)
		// TODO: if locked (parent locked?) return multistatus with locked error as propstat
		return
	}

	paths := []string{path}
	if depth == "1" {
		// fetch all files if directory
		// TODO: respect []includes

		if s.pathIsDirectory(path) {
			for _, p := range s.directoryContents(path) {
				paths = append(paths, path+"/"+p)
			}
		}
	}

	buf := new(bytes.Buffer)
	buf.WriteString(`<?xml version="1.0" encoding="utf-8"?>`)
	buf.WriteString(`<multistatus xmlns='DAV:'>`)

	// TODO: https?
	abs := "http://" + r.Host + s.TrimPrefix

	for _, p := range paths {
		// TODO
		// test locks/ authorization
		// if properties, show only given properties, else all
		// if propnames, return names of properties, else names and values

		propertiesNotFound := []string{}

		f, _ := s.Fs.Open(p)
		defer f.Close()
		fi, _ := f.Stat()

		buf.WriteString(`<response>`)
		buf.WriteString(`<href>` + abs + "/" + p + `</href>`)
		buf.WriteString(`<propstat>`)
		{
			buf.WriteString(`<prop>`)
			{
				//  TODO: make less ugly
				for _, prop := range properties {

					switch prop {
					case "creationdate":
						if propnames {
							buf.WriteString(`<` + prop + `/>`)
						} else {
							buf.WriteString(`<` + prop + `>`)
							buf.WriteString(fi.ModTime().Format("2006-01-02T15:04:05Z07:00"))
							buf.WriteString(`</` + prop + `>`)
						}
					case "getcontentlanguage":
						if propnames {
							buf.WriteString(`<` + prop + `/>`)
						} else {
							buf.WriteString(`<` + prop + `>`)
							buf.WriteString(`en`)
							buf.WriteString(`</` + prop + `>`)
						}
					case "getcontentlength":
						if fi.IsDir() {
						} else if propnames {
							buf.WriteString(`<` + prop + `/>`)
						} else {
							buf.WriteString(`<` + prop + `>`)
							buf.WriteString(strconv.FormatInt(int64(fi.Size()), 10))
							buf.WriteString(`</` + prop + `>`)
						}
					case "getcontenttype":
						if fi.IsDir() {
						} else if propnames {
							buf.WriteString(`<` + prop + `/>`)
						} else {
							buf.WriteString(`<` + prop + `>`)
							buf.WriteString(mime.TypeByExtension(filepath.Ext(fi.Name())))
							buf.WriteString(`</` + prop + `>`)
						}
					case "getlastmodified":
						if fi.IsDir() {
						} else if propnames {
							buf.WriteString(`<` + prop + `/>`)
						} else {
							buf.WriteString(`<` + prop + `>`)
							buf.WriteString(fi.ModTime().Format("Mon, 02 Jan 2006 15:04:05 MST"))
							buf.WriteString(`</` + prop + `>`)
						}
					case "resourcetype":
						if propnames || !fi.IsDir() {
							// ZODO: reson for all the ugliness
							buf.WriteString(`<` + prop + `/>`)
						} else {
							buf.WriteString(`<` + prop + `>`)
							buf.WriteString(`<collection/>`)
							buf.WriteString(`</` + prop + `>`)
						}
					case "displayname":
						if propnames {
							buf.WriteString(`<` + prop + `/>`)
						} else {
							buf.WriteString(`<` + prop + `>`)
							buf.WriteString(fi.Name())
							buf.WriteString(`</` + prop + `>`)
						}
					case "supportedlock":
						if propnames {
							buf.WriteString(`<` + prop + `/>`)
						} else {
							buf.WriteString(`<` + prop + `>`)
							buf.WriteString(`<lockentry><lockscope><exclusive/></lockscope><locktype><write/></locktype></lockentry>`)
							buf.WriteString(`<lockentry><lockscope><shared/></lockscope><locktype><write/></locktype></lockentry>`)
							buf.WriteString(`</` + prop + `>`)
						}

						// TODO: implement later at locks-stage
						// case "getetag": // not for dir
						// case "lockdiscovery":
					default:
						propertiesNotFound = append(propertiesNotFound, prop)
					}
				}
			}
			buf.WriteString(`</prop>`)
			buf.WriteString(`<status>HTTP/1.1 200 OK</status>`)
		}
		buf.WriteString(`</propstat>`)

		if len(propertiesNotFound) > 0 {
			buf.WriteString(`<propstat>`)
			{
				buf.WriteString(`<prop>`)
				{
					for _, prop := range propertiesNotFound {
						buf.WriteString(`<` + prop + `/>`)
					}
				}
				buf.WriteString(`</prop>`)
				buf.WriteString(`<status>HTTP/1.1 404 ` + StatusText(404) + `</status>`)
			}
			buf.WriteString(`</propstat>`)
		}

		buf.WriteString(`</response>`)
	}

	buf.WriteString(`</multistatus>`)

	w.WriteHeader(StatusMulti)
	w.Header().Set("Content-Length", string(buf.Len()))
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")

	buf.WriteTo(w)
	// TODO: possible write error is suppressed
}

// http://www.webdav.org/specs/rfc4918.html#METHOD_PROPPATCH
func (s *Server) doProppatch(w http.ResponseWriter, r *http.Request) {
	if s.ReadOnly {
		w.WriteHeader(StatusForbidden)
		return
	}

	if s.isLockedRequest(r) {
		w.WriteHeader(StatusLocked)
		return
	}

	// TODO: proppatch
	w.WriteHeader(StatusNotImplemented)
}

// http://www.webdav.org/specs/rfc4918.html#METHOD_MKCOL
func (s *Server) doMkcol(w http.ResponseWriter, r *http.Request) {
	if s.ReadOnly {
		w.WriteHeader(StatusForbidden)
		return
	}

	if s.isLockedRequest(r) {
		w.WriteHeader(StatusLocked)
		return
	}

	path := s.url2path(r.URL)
	if s.pathExists(path) {
		w.Header().Set("Allow", s.methodsAllowed(s.url2path(r.URL)))
		w.WriteHeader(StatusMethodNotAllowed)
		return
	}

	// MKCOL may contain messagebody, precise behavior is undefined
	if r.ContentLength > 0 {
		_, err := NodeFromXml(r.Body)
		if err != nil {
			w.WriteHeader(StatusBadRequest)
			return
		}

		w.WriteHeader(StatusUnsupportedMediaType)
		return
	}

	if err := s.Fs.Mkdir(path); err != nil {
		w.WriteHeader(StatusConflict)
		return
	}

	w.WriteHeader(StatusCreated)
	s.unlockResource(path)
}

// http://www.webdav.org/specs/rfc4918.html#rfc.section.9.4
func (s *Server) doGet(w http.ResponseWriter, r *http.Request) {
	s.serveResource(w, r, true)
}

// http://www.webdav.org/specs/rfc4918.html#rfc.section.9.4
func (s *Server) doHead(w http.ResponseWriter, r *http.Request) {
	s.serveResource(w, r, false)
}

// http://www.webdav.org/specs/rfc4918.html#METHOD_POST
func (s *Server) doPost(w http.ResponseWriter, r *http.Request) {
	s.doGet(w, r)
}

func (s *Server) serveResource(w http.ResponseWriter, r *http.Request, serveContent bool) {
	path := s.url2path(r.URL)

	f, err := s.Fs.Open(path)
	if err != nil {
		http.Error(w, r.RequestURI, StatusNotFound)
		return
	}
	defer f.Close()

	// TODO: what if path is collection?

	fi, err := f.Stat()
	if err != nil {
		http.Error(w, r.RequestURI, StatusNotFound)
		return
	}
	modTime := fi.ModTime()

	if serveContent {
		http.ServeContent(w, r, path, modTime, f)
	} else {
		// TODO: better way to send only head
		http.ServeContent(w, r, path, modTime, emptyFile{})
	}
}

// http://www.webdav.org/specs/rfc4918.html#METHOD_DELETE
func (s *Server) doDelete(w http.ResponseWriter, r *http.Request) {
	if s.ReadOnly {
		w.WriteHeader(StatusForbidden)
		return
	}

	if s.isLockedRequest(r) {
		w.WriteHeader(StatusLocked)
		return
	}

	s.deleteResource(s.url2path(r.URL), w, r, true)
}

func (s *Server) deleteResource(path string, w http.ResponseWriter, r *http.Request, setStatus bool) bool {
	ifHeader := r.Header.Get("If")
	lockToken := r.Header.Get("Lock-Token")

	if s.isLocked(path, ifHeader+lockToken) {
		w.WriteHeader(StatusLocked)
		return false
	}

	if !s.pathExists(path) {
		w.WriteHeader(StatusNotFound)
		return false
	}

	if !s.pathIsDirectory(path) {
		if err := s.Fs.Remove(path); err != nil {
			w.WriteHeader(StatusInternalServerError)
			return false
		}
	} else {
		// http://www.webdav.org/specs/rfc4918.html#delete-collections
		errors := map[string]int{}
		s.deleteCollection(path, w, r, errors)

		if err := s.Fs.Remove(path); err != nil {
			errors[path] = StatusInternalServerError
		}

		if len(errors) != 0 {
			// send multistatus
			abs := r.RequestURI

			buf := new(bytes.Buffer)
			buf.WriteString(`<?xml version="1.0" encoding="utf-8"?>`)
			buf.WriteString(`<multistatus xmlns='DAV:'>`)

			for p, e := range errors {
				buf.WriteString(`<response>`)
				buf.WriteString(`<href>` + abs + p + `</href>`)
				buf.WriteString(`<status>HTTP/1.1 ` + string(e) + ` ` + StatusText(e) + `</status>`)
				buf.WriteString(`</response>`)
			}

			buf.WriteString(`</multistatus>`)

			w.WriteHeader(StatusMulti)
			w.Header().Set("Content-Length", string(buf.Len()))
			w.Header().Set("Content-Type", "application/xml; charset=utf-8")
			buf.WriteTo(w)

			return false
		}
	}

	if setStatus {
		w.WriteHeader(StatusNoContent)
	}
	return true
}

func (s *Server) deleteCollection(path string, w http.ResponseWriter, r *http.Request, errors map[string]int) {
	ifHeader := r.Header.Get("If")
	lockToken := r.Header.Get("Lock-Token")

	for _, p := range s.directoryContents(path) {
		p = path + "/" + p

		if s.isLocked(p, ifHeader+lockToken) {
			errors[p] = StatusLocked
		} else {
			if s.pathIsDirectory(p) {
				s.deleteCollection(p, w, r, errors)
			}

			if err := s.Fs.Remove(p); err != nil {
				errors[p] = StatusInternalServerError
			}
		}
	}

}

// http://www.webdav.org/specs/rfc4918.html#METHOD_PUT
func (s *Server) doPut(w http.ResponseWriter, r *http.Request) {
	if s.ReadOnly {
		w.WriteHeader(StatusForbidden)
		return
	}

	if s.isLockedRequest(r) {
		w.WriteHeader(StatusLocked)
		return
	}

	path := s.url2path(r.URL)

	if s.pathIsDirectory(path) {
		// use MKCOL instead
		w.WriteHeader(StatusMethodNotAllowed)
		return
	}

	exists := s.pathExists(path)

	// TODO: content range / partial put

	// truncate file if exists
	file, err := s.Fs.Create(path)
	if err != nil {
		w.WriteHeader(StatusConflict)
		return
	}
	defer file.Close()

	if _, err := io.Copy(file, r.Body); err != nil {
		w.WriteHeader(StatusConflict)
	} else {
		if exists {
			w.WriteHeader(StatusNoContent)
		} else {
			w.WriteHeader(StatusCreated)
		}
	}

	s.unlockResource(path)
}

// http://www.webdav.org/specs/rfc4918.html#METHOD_COPY
func (s *Server) doCopy(w http.ResponseWriter, r *http.Request) {
	if s.ReadOnly {
		w.WriteHeader(StatusForbidden)
		return
	}

	s.copyResource(w, r)
}

// http://www.webdav.org/specs/rfc4918.html#METHOD_MOVE
func (s *Server) doMove(w http.ResponseWriter, r *http.Request) {
	if s.ReadOnly {
		w.WriteHeader(StatusForbidden)
		return
	}

	if s.isLockedRequest(r) {
		w.WriteHeader(StatusLocked)
		return
	}

	if s.copyResource(w, r) {
		// TODO: duplicate http-header sent?
		s.deleteResource(s.url2path(r.URL), w, r, false)
	}
}

func (s *Server) copyResource(w http.ResponseWriter, r *http.Request) bool {
	dest := r.Header.Get("Destination")
	if dest == "" {
		w.WriteHeader(StatusBadRequest)
		return false
	}

	d, err := url.Parse(dest)
	if err != nil {
		w.WriteHeader(StatusBadRequest)
		return false
	}
	// TODO: normalize dest?
	dest = s.url2path(d)
	source := s.url2path(r.URL)

	// source equals destination
	if source == dest {
		w.WriteHeader(StatusForbidden)
		return false
	}

	// destination must be same server/namespace as source
	if d.Host != r.Host ||
		!strings.HasPrefix(d.Path, s.TrimPrefix) ||
		!strings.HasPrefix(r.URL.Path, s.TrimPrefix) {

		w.WriteHeader(StatusBadGateway)
		return false
	}

	// TODO: needs to be tested? should be catched with error at CopyFile returning StatusConflict
	// currently only at depth=0 or non-collection copy
	/*
		parentDest := dest[:strings.LastIndex(dest, "/")]
		if !s.pathExists(parentDest) {
			w.WriteHeader(StatusConflict)
			return false
		}
	*/

	overwrite := r.Header.Get("Overwrite") != "F"
	exists := s.pathExists(dest)

	if overwrite {
		if exists {
			if !s.deleteResource(dest, w, r, false) {
				w.WriteHeader(StatusInternalServerError)
				return false
			}
		}
	} else {
		if exists {
			w.WriteHeader(StatusPreconditionFailed)
			return false
		}
	}

	if !s.pathIsDirectory(source) {
		if err := s.CopyFile(source, dest); err != nil {
			// TODO: always conflict? e.g. copy to non-existant path
			//w.WriteHeader(StatusInternalServerError)
			w.WriteHeader(StatusConflict)
			return false
		}
	} else if r.Header.Get("Depth") == "0" {
		// copy only collection, not its internal members
		// http://www.webdav.org/specs/rfc4918.html#copy.for.collections
		if err := s.Fs.Mkdir(dest); err != nil {
			w.WriteHeader(StatusConflict)
			return false
		}
	} else {
		// http://www.webdav.org/specs/rfc4918.html#copy.for.collections
		errors := map[string]int{}

		if err := s.Fs.Mkdir(dest); err != nil {
			errors[source] = StatusInternalServerError
		}

		s.copyCollection(source, dest, w, r, errors)

		if len(errors) != 0 {
			// send multistatus
			abs := r.RequestURI

			buf := new(bytes.Buffer)
			buf.WriteString(`<?xml version="1.0" encoding="utf-8"?>`)
			buf.WriteString(`<multistatus xmlns='DAV:'>`)

			for p, e := range errors {
				buf.WriteString(`<response>`)
				buf.WriteString(`<href>` + abs + p + `</href>`)
				buf.WriteString(`<status>HTTP/1.1 ` + string(e) + ` ` + StatusText(e) + `</status>`)
				buf.WriteString(`</response>`)
			}

			buf.WriteString(`</multistatus>`)

			w.WriteHeader(StatusMulti)
			w.Header().Set("Content-Length", string(buf.Len()))
			w.Header().Set("Content-Type", "application/xml; charset=utf-8")
			buf.WriteTo(w)

			return false
		}
	}

	// copy was successful
	if exists {
		w.WriteHeader(StatusNoContent)
	} else {
		w.WriteHeader(StatusCreated)
	}

	s.unlockResource(dest)
	return true
}

func (s *Server) CopyFile(source, dest string) error {
	// open source file
	fs, err := s.Fs.Open(source)
	if err != nil {
		return err
	}
	defer fs.Close()

	// open destination file
	fd, err := s.Fs.Create(dest)
	if err != nil {
		return err
	}
	defer fd.Close()

	// copy file contents
	if _, err := io.Copy(fd, fs); err != nil {
		return err
	}

	// TODO: copy file stats? http://www.webdav.org/specs/rfc4918.html#copy.for.properties

	return nil
}

func (s *Server) copyCollection(source, dest string, w http.ResponseWriter, r *http.Request, errors map[string]int) {
	ifHeader := r.Header.Get("If")
	lockToken := r.Header.Get("Lock-Token")

	for _, sub := range s.directoryContents(source) {
		ssub := source + "/" + sub
		dsub := dest + "/" + sub

		if s.isLocked(ssub, ifHeader+lockToken) {
			errors[ssub] = StatusLocked
		} else {
			if s.pathIsDirectory(ssub) {
				if err := s.Fs.Mkdir(dsub); err != nil {
					errors[ssub] = StatusInternalServerError
				}

				s.copyCollection(ssub, dsub, w, r, errors)
			} else {
				if err := s.CopyFile(ssub, dsub); err != nil {
					errors[ssub] = StatusInternalServerError
				}
			}
		}
	}
}

/*func (s *Server) _lock_unlock_parse(body string) (map[string]string, error) {
	node, err := NodeFromXmlString(body)
	if err != nil {
		return nil, err
	}

	data := make(map[string]string)
	if node != nil {
		if node.Name.Local != "lockinfo" {
			node = node.FirstChildren("lockinfo")
		}
		if node == nil {
			return nil, errors.New("not lockinfo element")
		}

		data["lockscope"] = node.FirstChildren("lockscope").Children[0].Name.Local

		data["locktype"] = node.FirstChildren("locktype").Children[0].Name.Local

		data["lockowner"] = node.FirstChildren("owner").Children[0].Value[7:]
	}
	return data, nil
}*/

func (s *Server) _lock_unlock_create(lock *Lock, depth string) (string, string) {
	//lock := &Lock{uri: uri, creator: creator}
	iscollection := (lock.uri[len(lock.uri)-1] == '/') //# very dumb collection check

	result := ""
	if depth == "infinity" && iscollection {
		//# locking of children/collections not yet supported
		//pass
	}

	if !s.isLocked(lock.uri, "") {
		s.setLock(lock)
	}

	//# because we do not handle children we leave result empty
	return lock.token, result
}

/*
  LOCK /workspace/webdav/proposal.doc HTTP/1.1
  Host: example.com
  Timeout: Infinite, Second-4100000000
  Content-Type: application/xml; charset="utf-8"
  Content-Length: xxxx
  Authorization: Digest username="ejw",
    realm="ejw@example.com", nonce="...",
    uri="/workspace/webdav/proposal.doc",
    response="...", opaque="..."

  <?xml version="1.0" encoding="utf-8" ?>
  <D:lockinfo xmlns:D='DAV:'>
    <D:lockscope><D:exclusive/></D:lockscope>
    <D:locktype><D:write/></D:locktype>
    <D:owner>
      <D:href>http://example.org/~ejw/contact.html</D:href>
    </D:owner>
  </D:lockinfo>
*/

func (s *Server) doLock(w http.ResponseWriter, r *http.Request) {
	if s.ReadOnly {
		w.WriteHeader(StatusForbidden)
		return
	}

	if s.isLockedRequest(r) {
		w.WriteHeader(StatusLocked)
		return
	}

	//dc = self.IFACE_CLASS

	// qlog.Info("LOCKing resource %s", r.Header)

	bbody, err := ioutil.ReadAll(r.Body)
	if err != nil {
		fmt.Println(err)
		w.WriteHeader(500)
		return
	}
	var body = string(bbody)

	depth := "infinity"
	if r.Header.Get("Depth") != "" {
		depth = r.Header.Get("Depth")
	}

	//uri = urlparse.urljoin(self.get_baseuri(dc), self.path)
	//uri = urllib.unquote(uri)
	uri := r.RequestURI
	// qlog.Info("do_LOCK: uri = %s", uri)

	ifheader := r.Header.Get("If")
	alreadylocked := s.isLocked(uri, ifheader)
	// qlog.Info("do_LOCK: alreadylocked = %s", alreadylocked)

	if body != "" && alreadylocked {
		//# Full LOCK request but resource already locked
		//self.responses[423] = ('Locked', 'Already locked')
		w.WriteHeader(423)
		return
	} else if body != "" && ifheader == "" {
		//# LOCK with XML information
		//fmt.Println("body:", body)
		lock, err := ParseLockString(body)
		//data, err := s._lock_unlock_parse(body)
		if err != nil {
			fmt.Println(err)
			w.WriteHeader(500)
			return
		}
		lock.timeout = ParseTimeOut(r)
		lock.uri = r.RequestURI
		lock.token = generateToken()
		//fmt.Println("lock:", data)
		token, result := s._lock_unlock_create(lock, depth)

		if result != "" {
			w.Write([]byte(result))
			w.Header().Set("Content-Type", "text/xml; charset=utf-8")
			w.WriteHeader(207)
		} else {
			//lock := s.getLock(token)
			w.Header().Set("Lock-Token", fmt.Sprintf("<opaquelocktoken:%s>", token))
			w.Header().Set("Content-Type", "text/xml; charset=utf-8")
			output := lock.asXML("D:", false)
			fmt.Println("output:", output)
			w.Write([]byte(output))
			//w.WriteHeader(200)
		}
	} else {
		//d# refresh request - refresh lock timeout
		taglist := IfParser(ifheader)
		var found bool
		for _, tag := range taglist {
			for _, listitem := range tag.list {
				token := tokenFinder(listitem)
				if token != "" && s.hasLock(token) {
					lock := s.getLock(token)
					timeout := "Infinite"
					if r.Header.Get("Timeout") != "" {
						timeout = r.Header.Get("Timeout")
					}
					to, _ := strconv.Atoi(timeout)
					lock.SetTimeout(time.Duration(to)) //# automatically refreshes
					found = true

					w.WriteHeader(200)
					w.Write([]byte(lock.asXML("", true)))
					w.Header().Set("Content-Type", "text/xml; encoding=utf-8")
					break
				}
			}
			if found {
				break
			}
		}
		//# we didn't find any of the tokens mentioned - means
		//# that table was cleared or another error
		if !found {
			w.WriteHeader(412) //a# precondition failed
		}
	}
}

// takes a string like '<opaquelocktoken:afsdfadfadf> and returns the token
// part.
func tokenFinder(token string) string {
	if token == "" {
		return ""
	}
	if token[0] == '[' {
		return ""
	}
	if token[0] == '<' {
		token = token[1 : len(token)-1]
	}
	return token[strings.Index(token, ":")+1:]
}

func (s *Server) doUnlock(w http.ResponseWriter, r *http.Request) {
	/*if s.ReadOnly {
		w.WriteHeader(StatusForbidden)
		return
	}

	if s.isLockedRequest(r) {
		w.WriteHeader(StatusLocked)
		return
	}

	// TODO: unlock
	w.WriteHeader(StatusNotImplemented)
	return*/

	//dc = self.IFACE_CLASS

	//if self._config.DAV.getboolean('verbose') is True:
	// qlog.Info("UNLOCKing resource", r.Header)

	//uri := urlparse.urljoin(self.get_baseuri(dc), self.path)
	//uri = urllib.unquote(uri)
	uri := r.RequestURI

	// check lock token - must contain a dash
	lockToken := r.Header.Get("Lock-Token")
	if !strings.Contains(lockToken, "-") {
		w.WriteHeader(400)
		return
	}

	ifHeader := r.Header.Get("If")
	token := tokenFinder(lockToken)
	if s.isLocked(uri, ifHeader) {
		s.delLock(token)
	}

	w.WriteHeader(204)
	//self.send_body(None, '204', 'Ok', 'Ok')
}

func (s *Server) doOptions(w http.ResponseWriter, r *http.Request) {
	// http://www.webdav.org/specs/rfc4918.html#dav.compliance.classes
	w.Header().Set("DAV", "1, 2")

	w.Header().Set("Allow", s.methodsAllowed(s.url2path(r.URL)))
	w.Header().Set("MS-Author-Via", "DAV")
}

package webdav

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

type Lock struct {
	uri      string
	creator  string
	owner    string
	depth    int
	timeout  TimeOut
	typ      string
	scope    string
	token    string
	Modified time.Time
}

func NewLock(uri, creator, owner string) *Lock {
	return &Lock{
		uri,
		creator,
		owner,
		0,
		0,
		"write",
		"exclusive",
		generateToken(),
		time.Now(),
	}
}

// parse a lock from a http request
func ParseLockString(body string) (*Lock, error) {
	node, err := NodeFromXmlString(body)
	if err != nil {
		return nil, err
	}

	if node == nil {
		return nil, errors.New("no found node")
	}

	lock := new(Lock)

	if node.Name.Local != "lockinfo" {
		node = node.FirstChildren("lockinfo")
	}
	if node == nil {
		return nil, errors.New("not lockinfo element")
	}

	lock.scope = node.FirstChildren("lockscope").Children[0].Name.Local

	lock.typ = node.FirstChildren("locktype").Children[0].Name.Local

	lock.owner = node.FirstChildren("owner").Children[0].Value

	return lock, nil
}

func (lock *Lock) Refresh() {
	lock.Modified = time.Now()
}

func (lock *Lock) IsValid() bool {
	return time.Duration(lock.timeout) > time.Now().Sub(lock.Modified)
}

func (lock *Lock) GetTimeout() TimeOut {
	return lock.timeout
}

func (lock *Lock) SetTimeout(timeout time.Duration) {
	lock.timeout = TimeOut(timeout)
	lock.Modified = time.Now()
}

func (lock *Lock) asXML(namespace string, discover bool) string {
	//owner_str = lock.owner
	//owner_str = "".join([node.toxml() for node in self.owner[0].childNodes])

	base := fmt.Sprintf(`<%[1]s:activelock>
             <%[1]s:locktype><%[1]s:%[2]s/></%[1]s:locktype>
             <%[1]s:lockscope><%[1]s:%[3]s/></%[1]s:lockscope>
             <%[1]s:depth>%[4]d</%[1]s:depth>
             <%[1]s:owner>%[5]s</%[1]s:owner>
             <%[1]s:timeout>%[6]s</%[1]s:timeout>
             <%[1]s:locktoken>
             <%[1]s:href>opaquelocktoken:%[7]s</%[1]s:href>
             </%[1]s:locktoken>
             <%[1]s:lockroot>
             <%[1]s:href>%[8]s</%[1]s:href>
             </%[1]s:lockroot>
             </%[1]s:activelock>
             `, strings.Trim(namespace, ":"),
		lock.typ,
		lock.scope,
		lock.depth,
		lock.owner,
		lock.GetTimeout(),
		lock.token,
		lock.uri,
	)

	if discover {
		return base
	}

	return fmt.Sprintf(`<?xml version="1.0" encoding="utf-8" ?>
<D:prop xmlns:d="DAV:">
 <D:lockdiscovery>
  %s
 </D:lockdiscovery>
</D:prop>`, base)
}

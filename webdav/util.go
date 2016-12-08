package webdav

import (
	"fmt"
	"math/rand"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Depth string

var (
	NoDepth       = Depth("")
	Depth0        = Depth("0")
	Depth1        = Depth("1")
	DepthInfinity = Depth("infinity")
)

func ParseDepth(r *http.Request) Depth {
	return Depth(r.Header.Get("Depth"))
}

type LockToken string

var (
	NoLockToken = LockToken("")
)

func GenToken() LockToken {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	return LockToken(fmt.Sprintf("%s-%s-00105A989226:%.03f",
		r.Int31(), r.Int31(), time.Now().UnixNano()))
}

func ParseToken(r *http.Request) LockToken {
	return ""
}

var IfHdr = regexp.MustCompile(`(?P<resource><.+?>)?\s*\((?P<listitem>[^)]+)\)`)

var ListItem = regexp.MustCompile(
	`(?P<not>not)?\s*(?P<listitem><[a-zA-Z]+:[^>]*>|\[.*?\])`)

type TagList struct {
	resource string
	list     []string
	NOTTED   int
}

func IfParser(hdr string) []*TagList {
	out := make([]*TagList, 0)
	/*i := 0
	  for {
	      m := IfHdr.FindString(hdr[i:])
	      if m == ""{
	       break
	   	}

	      i = i + m.end()
	      tag := new(TagList)
	      tag.resource = m.group("resource")
	      // We need to delete < >
	      if tag.resource != "" {
	          tag.resource = tag.resource[1:-1]
	      }
	      listitem = m.group("listitem")
	      tag.NOTTED, tag.list = ListParser(listitem)
	      append(out, tag)
	  }*/

	return out
}

const (
	infinite = "Infinite"
)

type TimeOut time.Duration

func (to TimeOut) String() string {
	fmt.Println("===", time.Duration(to), "===")
	if int64(to) == 0 {
		return infinite
	}
	return fmt.Sprintf("Second-%d", time.Duration(to)/time.Second)
}

// Infinite, Second-4100000000
func ParseTimeOut(req *http.Request) TimeOut {
	tm := req.Header.Get("Timeout")
	if tm == "" || tm == infinite {
		return TimeOut(0)
	}

	ss := strings.Split(tm, "-")
	if len(ss) != 2 {
		return TimeOut(0)
	}

	a, err := strconv.Atoi(ss[1])
	if err != nil {
		return TimeOut(0)
	}
	return TimeOut(time.Duration(a) * time.Second)
}

func IsOverwrite(req *http.Request) bool {
	ow := req.Header.Get("Overwrite")
	return ow == "T" || ow == ""
}

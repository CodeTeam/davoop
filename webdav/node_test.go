package webdav

import (
	"fmt"
	"strings"
	"testing"
)

var txt = `<?xml version="1.0" encoding="utf-8" ?>
<D:lockinfo xmlns:D="DAV:">
<D:lockscope><D:exclusive/></D:lockscope>
<D:locktype><D:write/></D:locktype>
<D:owner>
<D:href>mailto:xiaolunwen@gmail.com</D:href>
</D:owner>
</D:lockinfo>`

func TestNodeFromXml(t *testing.T) {
	rd := strings.NewReader(txt)
	node, err := NodeFromXml(rd)
	if err != nil {
		t.Error(err)
	}

	fmt.Println("node.Name.Local", node.Name.Local)
	fmt.Println("node.Name.Space", node.Name.Space)
	fmt.Println(node.FirstChildren("owner"))
}

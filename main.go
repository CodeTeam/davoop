package main

import (
	"log"
	"net/http"

	"fmt"

	"github.com/codeteam/davoop/davoop"
	"github.com/codeteam/davoop/webdav"
	"github.com/kelseyhightower/envconfig"
)

type Configuration struct {
	NameNode string
	User     string
	Path     string `default:"/"`

	Addr string `default:":8080"`

	Listing  bool `default:"true"`
	ReadOnly bool `default:"false"`
}

func main() {
	var s Configuration
	err := envconfig.Process("davoop", &s)
	if err != nil {
		log.Fatal(err.Error())
	}

	fmt.Println(s)

	fs, err := davoop.NewHDFSDir(s.NameNode, s.User, s.Path)
	if err != nil {
		panic(err)
	}

	http.Handle("/", &webdav.Server{
		Fs:         fs,
		TrimPrefix: "/",
		Listings:   s.Listing,
		ReadOnly:   s.ReadOnly,
	})

	// log.Println("Listening on http://127.0.0.1:8080")
	log.Fatal(http.ListenAndServe(s.Addr, nil))
}

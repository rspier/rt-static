package main

/*
Copyright 2019 Google LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/rspier/rt-static/data"
	"github.com/rspier/rt-static/web"

	"github.com/golang/glog"
)

var (
	dataPath  = flag.String("data", "/big/rt-static/out/", "path to json data")
	indexPath = flag.String("index", filepath.Join(*dataPath, "index.bleve"), "path to bleve index")
	port      = flag.Int("port", 8080, "port to listen on")
	prefix    = flag.String("prefix", "", "URL Prefix")
	title     = flag.String("title", "", "Title")
)

func waitForFile(f string, r int, d time.Duration) error {
	for c := 0; c < r; c++ {
		_, err := os.Stat(f)
		if err == nil {
			return nil
		}
		glog.Infof("file %q not found after %v", f, time.Duration(c)*d)
		time.Sleep(d)
	}
	return fmt.Errorf("file %q still doesn't exist after waiting %v", f, d*time.Duration(r))

}

func main() {
	flag.Parse()

	// Allow for the data files not to exist at start up (for example,
	// if they're being synced from elsehwere.)
	err := waitForFile(filepath.Join(*indexPath, "store"), 10, 30*time.Second)
	if err != nil {
		glog.Fatal(err)
	}

	data, err := data.New(*dataPath, *indexPath)
	defer data.Close()
	if err != nil {
		glog.Fatal(err)
	}

	s := &web.Server{Prefix: *prefix, Tix: data, Title: *title}
	r := s.NewRouter()
	http.Handle("/", r)

	glog.Infof("Listening on port %v", *port)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", *port), nil))
}

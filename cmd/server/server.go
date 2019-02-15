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
	"path/filepath"

	"github.com/rspier/rt-static/data"
	"github.com/rspier/rt-static/web"

	"github.com/golang/glog"
)

var (
	dataPath  = flag.String("data", "/big/rt-static/out/", "path to json data")
	indexPath = flag.String("index", filepath.Join(*dataPath, "index.bleve"), "path to bleve index")
	port      = flag.Int("port", 8080, "port to listen on")
	prefix    = flag.String("prefix", "", "URL Prefix")
)

func main() {
	flag.Parse()

	data, err := data.New(*dataPath, *indexPath)
	defer data.Close()
	if err != nil {
		log.Fatal(err)
	}

	r := web.NewRouter(data, *prefix)
	http.Handle("/", r)

	glog.Infof("Listening on port %v", *port)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", *port), nil))
}

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
	"archive/zip"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rspier/rt-static/data"
	"github.com/rspier/rt-static/web"

	"github.com/golang/glog"
)

const snapshotFormat = "2006-01-02T15:04"

var (
	dataPath     = flag.String("data", "/big/rt-static/out/", "path to json data")
	indexPath    = flag.String("index", filepath.Join(*dataPath, "index.bleve"), "path to bleve index")
	port         = flag.Int("port", 8080, "port to listen on")
	prefix       = flag.String("prefix", "", "URL Prefix")
	site         = flag.String("site", "Perl 5 RT Archive", "Site Title")
	shortSite    = flag.String("shortsite", "Perl 5", "Short name of Site")
	gitHubPrefix = flag.String("githubprefix", "https://github.com/perl/perl5", "Prefix of GitHub links")
	staticDir    = flag.String("dir", "web/static", "the directory to serve files from. Defaults to web/static")
	snapshotTime = flag.String("snapshot", "", "when was the data archive created: "+snapshotFormat)
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

// extract the index.bleve directory from the provided zipfile
func extractIndexBleve(filename string) (string, error) {
	z, err := zip.OpenReader(filename)
	if err != nil {
		return "", err
	}
	defer z.Close()

	d, err := ioutil.TempDir("", "bleve")
	if err != nil {
		return "", err
	}

	db := filepath.Join(d, "index.bleve")
	err = os.Mkdir(db, 0700)
	if err != nil {
		return "", err
	}

	for _, f := range z.File {
		if !strings.HasPrefix(f.Name, "index.bleve") {
			continue
		}
		if f.FileInfo().IsDir() {
			continue
		}

		in, err := f.Open()
		if err != nil {
			return "", err
		}
		defer in.Close()

		out, err := os.OpenFile(filepath.Join(d, f.Name), os.O_CREATE|os.O_WRONLY, 0700)
		if err != nil {
			return "", err
		}
		defer out.Close()

		_, err = io.Copy(out, in)
		if err != nil {
			return "", err
		}
	}
	return db, nil
}

func main() {
	flag.Parse()
	var err error

	var sTime time.Time
	if *snapshotTime != "" {
		sTime, err = time.Parse(snapshotFormat, *snapshotTime)
		if err != nil {
			glog.Fatal(err)
		}
	}

	if strings.HasSuffix(*indexPath, ".zip") {
		*indexPath, err = extractIndexBleve(*indexPath)
		if err != nil {
			glog.Fatal(err)
		}
	}

	// Allow for the data files not to exist at start up (for example,
	// if they're being synced from elsehwere.)
	err = waitForFile(filepath.Join(*indexPath, "store"), 10, 30*time.Second)
	if err != nil {
		glog.Fatal(err)
	}

	data, err := data.New(*dataPath, *indexPath)
	defer data.Close()
	if err != nil {
		glog.Fatal(err)
	}

	s := &web.Server{
		Prefix:       *prefix,
		Tix:          data,
		Site:         *site,
		ShortSite:    *shortSite,
		StaticDir:    *staticDir,
		GitHubPrefix: *gitHubPrefix,
		SnapshotTime: sTime,
	}
	r := s.NewRouter()
	http.Handle("/", r)

	glog.Infof("Listening on port %v", *port)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", *port), nil))
}

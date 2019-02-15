// cli is a tool for searching our bleve index
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
	"path/filepath"
	"strings"

	"github.com/blevesearch/bleve"

	"github.com/blevesearch/bleve/search/highlight/highlighter/ansi"

	"github.com/rspier/rt-static/data"
)

var (
	dataPath  = flag.String("data", "/big/rt-static/out/", "path to json data")
	indexPath = flag.String("index", filepath.Join(*dataPath, "index.bleve"), "path to bleve index")
)

func main() {
	flag.Parse()

	data, err := data.New(*dataPath, *indexPath)
	defer data.Close()
	if err != nil {
		log.Fatal(err)
	}

	q := "status:open"
	if len(flag.Args()) > 0 {
		q = strings.Join(flag.Args(), " ")
	}

	query := bleve.NewQueryStringQuery(q)
	sr := bleve.NewSearchRequestOptions(query, 10, 0, false)
	sr.Fields = []string{"id", "status", "subject"}
	sr.Highlight = bleve.NewHighlightWithStyle(ansi.Name)

	sr.SortBy([]string{"-id"})
	searchResults, err := data.Index.Search(sr)
	if err != nil {
		fmt.Println(err)
		return
	}

	// Sometimes the Fragment is empty.  Something to do with Unicode?
	for _, d := range searchResults.Hits {
		s := strings.Join(d.Fragments["subject"], "") // normally just one
		if len(s) == 0 {
			s = d.Fields["subject"].(string)
		}
		fmt.Printf("%.0f\t%s\t(%s)\n", d.Fields["id"], s, d.Fields["status"])
	}

}

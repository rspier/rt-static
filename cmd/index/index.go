// index generates a json index and bleve search index
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
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"sync"

	"github.com/blevesearch/bleve"
	"github.com/blevesearch/bleve/mapping"
	"github.com/golang/glog"
	"github.com/schollz/progressbar/v2"
	"golang.org/x/sync/semaphore"
)

var (
	dataPath  = flag.String("data", "/big/rt-static/out/", "path to json data index")
	out       = flag.String("outdir", *dataPath, "path to write bleve data to")
	bleveName = flag.String("blevename", "index.bleve", "name of bleve dir")
	batchSize = flag.Int("batch", 1000, "bleve indexing batch size")
	// In early testing (without a numeric field) batchSize=100 takes about a minute,
	// batchSize=500 takes 26 seconds, batchSize=1000 takes 10 seconds.
	parallelRead = flag.Int64("parallelread", 16, "number of ticket files to read at once")
)

// ticket represents the fields of a ticket we're interested in for indexing

type ticket struct {
	ID           string `json:"Id"`
	Status       string
	Subject      string
	Transactions []struct {
		ID          string `json:"Id"`
		Attachments []struct {
			ID string `json:"Id"`
		}
	}
}

func processFile(path string) (*ticket, error) {
	b, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var t ticket
	err = json.Unmarshal(b, &t)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func readTickets(root string) []ticket {
	var tickets []ticket

	// Consider using the reader interfaces instead of reimplementing the parsing.
	files, err := filepath.Glob(filepath.Join(root, "*.json"))
	if err != nil {
		log.Fatal(err)
	}
	bar := progressbar.NewOptions(len(files), progressbar.OptionSetDescription("reading tickets"))
	var wg sync.WaitGroup
	var mu sync.Mutex
	sem := semaphore.NewWeighted(*parallelRead)

	var fileRe = regexp.MustCompile(`\d+\.json$`)

	for _, path := range files {
		wg.Add(1)
		_ = sem.Acquire(context.Background(), 1)
		go func(path string) {
			defer wg.Done()
			defer sem.Release(1)
			if !fileRe.MatchString(path) {
				return
			}

			t, err := processFile(path)
			if err != nil {
				log.Fatalf("%v: %v", path, err)
			}

			bar.Add(1)

			mu.Lock()
			tickets = append(tickets, *t)
			mu.Unlock()

		}(path)
	}
	wg.Wait()

	sort.Slice(tickets, func(i, j int) bool {
		ii, _ := strconv.Atoi(tickets[i].ID)
		jj, _ := strconv.Atoi(tickets[j].ID)
		return ii < jj
	})

	bar.Finish()
	bar.Clear()

	return tickets
}

func setupTicketMapping(m *mapping.IndexMappingImpl) {
	ticketMapping := bleve.NewDocumentMapping()
	m.AddDocumentMapping("ticket", ticketMapping)

	// id being a number slows down the indexing by 2-3x, but will let us do range searches.
	idFieldMapping := bleve.NewNumericFieldMapping()
	ticketMapping.AddFieldMappingsAt("id", idFieldMapping)
	subjectFieldMapping := bleve.NewTextFieldMapping()
	subjectFieldMapping.Analyzer = "en"
	subjectFieldMapping.IncludeTermVectors = true
	subjectFieldMapping.Store = true
	ticketMapping.AddFieldMappingsAt("subject", subjectFieldMapping)
	statusFieldMapping := bleve.NewTextFieldMapping()
	statusFieldMapping.Analyzer = "en"
	ticketMapping.AddFieldMappingsAt("status", statusFieldMapping)
}

/*
// this is here as the start of possibly indexing message content too
func setupMessageMapping(m *mapping.IndexMappingImpl) {
	messageMapping := bleve.NewDocumentMapping()
	m.AddDocumentMapping("message", messageMapping)

	tidFM := bleve.NewNumericFieldMapping()
	messageMapping.AddFieldMappingsAt("ticket_id", tidFM)
	contentFieldMapping := bleve.NewTextFieldMapping()
	contentFieldMapping.Analyzer = "en"
	// not setting IncludeTermVectors and Store because they're going to blow up the index size
	messageMapping.AddFieldMappingsAt("content", contentFieldMapping)
}
*/

type indexedTicket struct {
	ID      int    `json:"id"`
	Status  string `json:"status"`
	Subject string `json:"subject"`
}

func (indexedTicket) BleveType() string {
	return "ticket"
}

func buildBleveIndex(tickets []ticket, out string) error {
	m := bleve.NewIndexMapping()
	setupTicketMapping(m)
	//setupMessageMapping(m)

	index, err := bleve.New(out, m)
	if err != nil {
		return err
	}
	defer index.Close()

	pb := progressbar.NewOptions(len(tickets), progressbar.OptionSetDescription("building bleve"))

	batch := index.NewBatch()
	for i, tick := range tickets {
		pb.Add(1)

		id, err := strconv.Atoi(tick.ID)
		if err != nil {
			glog.Errorf("Atoi(%v) failed, skipping: %v", tick.ID, err)
			continue
		}
		data := indexedTicket{
			id, tick.Status, tick.Subject,
		}
		batch.Index(tick.ID, data)
		if i%*batchSize == 0 {
			index.Batch(batch)
			batch.Reset()
		}
	}
	index.Batch(batch) // index the final batch

	pb.Finish()
	pb.Clear()

	return nil
}

func writeIndexJSON(tickets []ticket, fn string) error { // Consider replacing this with a streaming encoder.
	b, err := json.Marshal(tickets)
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(fn, b, 0700)
	if err != nil {
		return err
	}
	return nil
}

func main() {
	flag.Parse()

	tickets := readTickets(*dataPath)

	outIndex := filepath.Join(*out, "index.json")
	outBleve := filepath.Join(*out, *bleveName)

	fmt.Printf("outputs:\n %s\n %s\ntickets: %d\n", outIndex, outBleve, len(tickets))

	err := writeIndexJSON(tickets, outIndex)
	if err != nil {
		log.Fatal(err)
	}

	err = buildBleveIndex(tickets, outBleve)
	if err != nil {
		log.Fatal(err)
	}
}

package data

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
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"reflect"
	"strings"

	"github.com/blevesearch/bleve"
	"github.com/golang/glog"
	"github.com/rspier/rt-static/readers"
)

// TicketSource describes the interface of the ticket reader classes we use.
type TicketSource interface {
	GetTicket(id string) (interface{}, error)
	GetJSON(id string) (io.ReadCloser, error)
	GetFile(id string) (io.ReadCloser, error)
}

// TODO: fixme data.Data stutters
type Data struct {
	// attachmentMetaMap maps between AttachmentId and and AttachmentMeta struct.
	ts                TicketSource
	attachmentMetaMap map[string]AttachmentMeta
	ticketIndex       []*IndexTicket
	rtGitHubMap       map[string]string
	Index             bleve.Index
	Merged            map[string]string
}

func New(dataPath string, indexPath string) (*Data, error) {
	var ticketSource TicketSource
	var err error
	if strings.HasSuffix(dataPath, ".zip") {
		ticketSource, err = readers.NewZipReader(dataPath)
	} else {
		ticketSource, err = readers.NewFileReader(dataPath)
	}
	if err != nil {
		log.Fatal(err)
	}
	glog.Info("done setting up ticketsource")
	index, err := bleve.Open(indexPath)
	if err != nil {
		log.Fatal(err)
	}
	glog.Info("done opening bleve")
	d := Data{ts: ticketSource, Index: index}

	err = d.newIndex()
	if err != nil {
		return nil, err
	}

	err = d.newRTGitHubMap()
	if err != nil {
		return nil, err
	}

	err = d.newMerged()
	if err != nil {
		return nil, err
	}

	return &d, nil
}

func (d *Data) Close() {
	d.Index.Close()
}

func (d *Data) newIndex() error {
	fh, err := d.ts.GetJSON("index")
	if err != nil {
		return err
	}
	defer fh.Close()
	err = d.LoadIndex(fh)
	if err != nil {
		log.Fatal(err)
	}
	return nil
}

// RTGitHubCSV returns the filename for the mapping of tickets from RT to GitHub
const RTGitHubCSV = "rtgithub.csv"

// RTGitHubCSV returns a io.ReadCloser pointing to the rtgithub.csv file
func (d *Data) RTGitHubCSV() (io.ReadCloser, error) {
	return d.ts.GetFile(RTGitHubCSV)
}

func (d *Data) newRTGitHubMap() error {
	d.rtGitHubMap = make(map[string]string)
	fh, err := d.ts.GetFile(RTGitHubCSV)
	if errors.Is(err, os.ErrNotExist) {
		// this map is optional, but definitely nice to have
		return nil
	}
	if err != nil {
		return err
	}
	defer fh.Close()
	err = d.LoadRTGitHubMap(fh)
	if err != nil {
		// return err ?
		log.Fatal(err)
	}
	return nil
}

func (d *Data) newMerged() error {
	d.Merged = make(map[string]string)
	fh, err := d.ts.GetJSON("merged")
	if errors.Is(err, os.ErrNotExist) {
		// this map is optional, but definitely nice to have
		return nil
	}
	if err != nil {
		return err
	}
	defer fh.Close()
	err = d.LoadMerged(fh)
	if err != nil {
		// return err ?
		log.Fatal(err)
	}
	return nil
}

type IndexTicket struct {
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

type AttachmentMeta struct {
	TicketID string
	// We could recompute the Offsets from the Ticket but storing them
	// saves a little time.
	TransactionOffset int
	AttachmentOffset  int
}

func (d *Data) processIndexTicket(t *IndexTicket) error {
	d.ticketIndex = append(d.ticketIndex, t)

	for trOff, tr := range t.Transactions {
		for attOff, att := range tr.Attachments {
			d.attachmentMetaMap[att.ID] = AttachmentMeta{
				TicketID:          t.ID,
				TransactionOffset: trOff,
				AttachmentOffset:  attOff,
			}
		}
	}
	return nil
}

// LoadRTGitHubMap loads the mapping of old ids to the new ones.
func (d *Data) LoadRTGitHubMap(fh io.Reader) error {
	c := csv.NewReader(fh)
	rs, err := c.ReadAll()
	if err != nil {
		return err
	}
	for _, row := range rs {
		d.rtGitHubMap[row[0]] = row[1]
	}
	return nil
}

func (d *Data) LoadMerged(fh io.Reader) error {
	j := json.NewDecoder(fh)
	return j.Decode(&d.Merged)
}

// LoadIndex loads an index.json file.
func (d *Data) LoadIndex(fh io.Reader) error {
	j := json.NewDecoder(fh)

	// read open bracket so the array elements are next
	_, err := j.Token()
	if err != nil {
		return err
	}

	d.attachmentMetaMap = make(map[string]AttachmentMeta)

	for j.More() {
		var t IndexTicket
		err := j.Decode(&t)
		if err != nil {
			return err
		}
		err = d.processIndexTicket(&t)
		if err != nil {
			return err
		}
	}
	// read closing bracket
	_, err = j.Token()
	if err != nil {
		return err
	}
	return nil
}

func (d *Data) GetTicket(id string) (interface{}, error) {
	t, err := d.ts.GetTicket(id)
	if err != nil {
		return t, err
	}
	// use reflection to add a GitHubIssue field.  Ticket should really be a proper type.
	g, _ := d.rtGitHubMap[id] // throw away ok, because we want the default value of "" if not found.
	v := reflect.ValueOf(t)
	v.SetMapIndex(reflect.ValueOf("GitHubIssue"), reflect.ValueOf(g))

	return t, nil
}

// GetAttachment returns the filename, content-type, and bytes of an attachment.
func (d *Data) GetAttachment(id string) (string, string, []byte, error) {
	attMeta, ok := d.attachmentMetaMap[id]
	if !ok {
		return "", "", nil, fmt.Errorf("can't find metadata for attachment %v", id)
	}

	tick, err := d.GetTicket(attMeta.TicketID)
	if err != nil {
		return "", "", nil, fmt.Errorf("getTIcket(%v): %v", attMeta.TicketID, err)
	}

	glog.Infof("Ticket: %q", attMeta.TicketID)
	toff := attMeta.TransactionOffset
	aoff := attMeta.AttachmentOffset

	t := tick.(map[string]interface{})
	ts := t["Transactions"].([]interface{})
	tr := ts[int(toff)].(map[string]interface{})
	atts := tr["Attachments"].([]interface{})
	att := atts[int(aoff)].(map[string]interface{})

	contentType := att["ContentType"].(string)
	filename := att["Filename"].(string)

	glog.Infof("Filename: %q", att["Filename"].(string))
	glog.Infof("Content Type: %q", att["ContentType"].(string))

	originalContent := att["OriginalContent"].(string)
	var content []byte
	if strings.HasPrefix(contentType, "text/") {
		content = []byte(originalContent)
	} else {
		content, err = base64.StdEncoding.DecodeString(originalContent)
		if err != nil {
			return "", "", nil, fmt.Errorf("can't decode attachment: %v", err)
		}
	}

	return filename, contentType, content, nil
}

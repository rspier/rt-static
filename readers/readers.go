package readers

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
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
)

func parseTicket(b []byte) (interface{}, error) {
	var d interface{}
	err := json.Unmarshal(b, &d)
	if err != nil {
		return nil, err
	}
	return d, nil
}

type fileReader struct {
	Root string
}

// NewFileReader creates a fileReader instance.
func NewFileReader(root string) (*fileReader, error) {
	return &fileReader{
		Root: root,
	}, nil
}

func (fr fileReader) GetReader(id string) (io.ReadCloser, error) {
	return os.Open(filepath.Join(fr.Root, string(id)+".json"))
}

func (fr fileReader) GetTicket(id string) (interface{}, error) {
	r, err := fr.GetReader(id)
	defer r.Close()
	if err != nil {
		return nil, err
	}
	b, err := ioutil.ReadAll(r)
	if err != nil {
		return nil, err
	}
	return parseTicket(b)
}

type zipReader struct {
	zipfile string
	rdr     *zip.ReadCloser
	Files   map[string]*zip.File
}

// NewZipReader opens a zipfile and creates a zipReader.
func NewZipReader(filename string) (*zipReader, error) {
	zr := &zipReader{
		zipfile: filename,
	}

	r, err := zip.OpenReader(filename)
	if err != nil {
		return nil, err
	}
	zr.rdr = r

	zr.Files = make(map[string]*zip.File)
	for _, f := range r.File {
		zr.Files[f.Name] = f
	}

	return zr, nil
}

func (zr *zipReader) GetReader(id string) (io.ReadCloser, error) {
	fn := fmt.Sprintf("%s.json", id)
	f, ok := zr.Files[fn]
	if !ok {
		return nil, fmt.Errorf("%v not found in %v", fn, zr.zipfile)
	}
	return f.Open()
}

func (zr *zipReader) GetTicket(id string) (interface{}, error) {
	r, err := zr.GetReader(id)
	defer r.Close()
	if err != nil {
		return nil, err
	}
	b, err := ioutil.ReadAll(r)
	if err != nil {
		return nil, err
	}

	if err != nil {
		return nil, err
	}
	return parseTicket(b)
}

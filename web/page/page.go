package page

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
	"html/template"
	"log"
	"net/http"
)

type Page struct {
	Site string
	// Title is defined in the template... would it be simpler if it was here?
	Content interface{}
}

func (p *Page) Render(w http.ResponseWriter, tmpl *template.Template) {
	err := tmpl.ExecuteTemplate(w, "_base", p)
	if err != nil {
		log.Printf("Rendering error: %v", err)
		http.Error(w, "Internal Error", 500)
	}
}

func New() *Page {
	return &Page{}
}

var commonSources = []string{
	"web/templates/_base.html",
}

func NewTemplate(name string, funcMap template.FuncMap, sources ...string) *template.Template {
	sources = append(sources, commonSources...)

	if funcMap == nil {
		funcMap = make(template.FuncMap)
	}
	// if there were funcs we needed on every page, implement defaultFunc?
	// for k, v := range defaultFuncs {
	// 	if _, ok := funcMap[k]; !ok { // don't override thigns in funcMap
	// 		funcMap[k] = v
	// 	}
	// }

	t := template.New(name).Funcs(funcMap)
	t = template.Must(t.ParseFiles(sources...)) // will panic on error
	return t
}

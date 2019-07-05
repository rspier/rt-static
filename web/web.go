// Package web contains web related code.
package web

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
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/rspier/rt-static/data"

	"github.com/blevesearch/bleve"
	"github.com/gorilla/mux"
)

var (
	tix    *data.Data
	prefix string
)

// NewRouter sets up the http.Handler s for our server.
func NewRouter(d *data.Data, pr string) http.Handler {
	log.Printf("starting server with prefix %q", pr)
	tix = d
	prefix = pr
	r := mux.NewRouter()

	// We should use http.StripPrefix instead of prepending pr, but it
	// wasn't working right, and requires logging changes to track the
	// pre-StripPrefix URL.
	r.HandleFunc(pr+"/", indexHandler)
	r.HandleFunc(pr+"/index.html", indexHandler)
	r.HandleFunc(pr+"/robots.txt", robotsTxtHandler)
	r.HandleFunc(pr+"/Ticket/Display.html", ticketHandler)
	r.HandleFunc(pr+"/Ticket/Attachment/{transactionID}/{attachmentID:[0-9]+}/{filename}", attachHandler)
	r.HandleFunc(pr+"/Search/Simple.html", searchHandler)

	return logWrap(http.TimeoutHandler(r, 10*time.Second, "response took too long"))
}

func logWrap(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rw := &responseWriter{ResponseWriter: w}
		h.ServeHTTP(rw, r)
		fmt.Printf("%v %v %v %v %v\n", time.Now().Format(time.RFC3339), r.RemoteAddr, r.Method, r.RequestURI, rw.status)
	})
}

// responseWriter intercepts the WriteHeader call so the status can be used for logging.
type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(status int) {
	rw.ResponseWriter.WriteHeader(status)
	rw.status = status
}

// Ticket is a struct that is used for search results
type Ticket struct {
	ID      string `json:"Id"`
	Status  string
	Subject string
}

var tmpl *template.Template

const (
	ticketTemplate = "ticket.html"
	indexTemplate  = "index.html"
	searchTemplate = "search.html"
)

func init() {
	var funcs = template.FuncMap{
		"obfuscateEmail": obfuscateEmail,
	}
	tmpl = template.Must(template.New("").Funcs(funcs).ParseGlob("web/templates/*.html"))
}

func elide(input string, show int) string {
	if len(input) <= show {
		return "..."
	}
	input = input[0:show]
	return input + "..."
}

func obfuscateEmail(emailI interface{}) string {
	// accept an interface{} to deal with the nil case easily.
	// Otherwise template gets unhappy.
	email, ok := emailI.(string)
	if !ok {
		return ""
	}
	if len(email) == 0 {
		return ""
	}
	if !strings.Contains(email, "@") {
		return email
	}
	parts := strings.SplitN(email, "@", 2)
	if len(parts) < 2 {
		parts = append(parts, "")
	}
	return elide(parts[0], 4) + "@" + elide(parts[1], 3)
}

func isNotFound(err error) bool {
	if err != nil {
		return strings.Contains(err.Error(), "no such file or directory")
	}
	return false
}

func indexHandler(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, fmt.Sprintf("%s/Search/Simple.html?q=status:*", prefix), http.StatusTemporaryRedirect)
}

func ticketHandler(w http.ResponseWriter, r *http.Request) {
	id := r.FormValue("id")
	d, err := tix.GetTicket(id)
	if isNotFound(err) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		log.Printf("GetTicket(%v): %v", id, err)
		http.Error(w, "Internal Error", 500)
		return
	}

	err = tmpl.ExecuteTemplate(w, ticketTemplate, d)
	if err != nil {
		log.Printf("ExecuteTemplate(for ticket %v): %v", id, err)
		http.Error(w, "Internal Error", 500)
		return
	}
}

func attachHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	attID := vars["attachmentID"]

	filename, contentType, content, err := tix.GetAttachment(attID)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	w.Header().Set("Content-Disposition",
		fmt.Sprintf("attachment; filename=%q", filename))
	w.Header().Set("Content-Type", contentType)
	w.Write(content)
}

func searchHandler(w http.ResponseWriter, r *http.Request) {
	var d struct {
		Query      string
		Tickets    []Ticket
		Start      uint64
		End        uint64
		PageSize   uint64
		Total      uint64
		Took       time.Duration
		Next, Prev string
		Sizes      []int
		Order      string
		Prefix     string
	}

	q := r.FormValue("q")
	d.Query = q
	d.Sizes = []int{10, 25, 50, 100}
	d.Prefix = prefix

	start, _ := strconv.ParseUint(r.FormValue("start"), 10, 64)  // ignore error, get 0
	pageSize, _ := strconv.ParseUint(r.FormValue("num"), 10, 64) // ignore error, get 0
	if pageSize == 0 {
		pageSize = 25
	} else if pageSize > 100 {
		pageSize = 25
	}

	order := r.FormValue("order")
	switch order {
	case "0", "1":
		break
	default:
		order = "0"
	}

	if q != "" {

		sr := bleve.NewSearchRequestOptions(bleve.NewQueryStringQuery(q), int(pageSize), int(start), false)

		if order == "0" {
			sr.SortBy([]string{"id"})
		} else {
			sr.SortBy([]string{"-id"})
		}

		sr.Fields = []string{"id", "status", "subject"}

		searchResults, err := tix.Index.Search(sr)
		if err != nil {
			http.Error(w, fmt.Sprintf("%v", err), 500)
			fmt.Println(err)
			return
		}

		for _, h := range searchResults.Hits {
			f := h.Fields
			d.Tickets = append(d.Tickets,
				Ticket{
					ID:      fmt.Sprintf("%.0f", f["id"].(float64)),
					Subject: f["subject"].(string),
					Status:  f["status"].(string),
				})
		}

		d.Total = searchResults.Total
		d.Took = searchResults.Took
		d.Start = start
		d.PageSize = pageSize
		d.End = start + pageSize
		if d.End > d.Total {
			d.End = d.Total
		}

		const params = "?q=%s&start=%d&num=%d&order=%s"
		if uint64(start+pageSize) < searchResults.Total {
			d.Next = fmt.Sprintf(params, url.QueryEscape(q), start+pageSize, pageSize, order)
		}
		prev := start - pageSize
		if prev >= 0 && prev < 999999999 { // mixing uint and int and subtraction is hard
			d.Prev = fmt.Sprintf(params, url.QueryEscape(q), prev, pageSize, order)
		}
	}

	err := tmpl.ExecuteTemplate(w, searchTemplate, d)
	if err != nil {
		log.Printf("%v", err)
		http.Error(w, fmt.Sprintf("%v", err), 500)
	}
}

func robotsTxtHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	// Disallow everything for now.
	w.Write([]byte(`User-agent: *
Disallow: /`))
}

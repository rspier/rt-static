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
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/rspier/rt-static/data"
	"github.com/rspier/rt-static/web/page"

	"github.com/blevesearch/bleve"
	"github.com/gorilla/mux"
)

// Server holds state for the webserver.
type Server struct {
	Tix          *data.Data
	Prefix       string
	Site         string
	ShortSite    string // Perl5 or Perl6
	StaticDir    string
	GitHubPrefix string // https://github.com/org/repo
}

// NewRouter sets up the http.Handler s for our server.
func (s *Server) NewRouter() http.Handler {
	log.Printf("starting server with prefix %q on port", s.Prefix)
	r := mux.NewRouter()

	// We should use http.StripPrefix instead of prepending pr, but it
	// wasn't working right, and requires logging changes to track the
	// pre-StripPrefix URL.
	r.HandleFunc("/", s.indexHandler)
	r.HandleFunc("/index.html", s.indexHandler)
	r.HandleFunc(s.Prefix+"/", s.indexHandler)
	r.HandleFunc(s.Prefix+"/index.html", s.indexHandler)
	r.HandleFunc("/robots.txt", s.robotsTxtHandler)
	r.HandleFunc(s.Prefix+"/Ticket/Display.html", s.ticketHandler)
	r.HandleFunc(s.Prefix+"/Ticket/Attachment/{transactionID}/{attachmentID:[0-9]+}/{filename}", s.attachHandler)
	r.HandleFunc(s.Prefix+"/Search/Simple.html", s.searchHandler)
	// route to serve static content
	r.PathPrefix(s.Prefix + "/static").Handler(http.StripPrefix(s.Prefix+"/static", http.FileServer(http.Dir(s.StaticDir))))

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
)

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

func statusToBadgeClass(status string) string {

	switch status {
	case "new":
		return "badge-primary"
	case "open":
		return "badge-info"
	case "resolved":
		return "badge-dark"
	case "pending release":
		return "badge-warning"
	case "rejected":
		return "badge-secondary"
	}
	return "badge-light"
}

func isNotFound(err error) bool {
	// error wrapping is better than string matching
	if errors.Is(err, os.ErrNotExist) {
		return true
	}
	if err != nil {
		return strings.Contains(err.Error(), "no such file or directory")
	}
	return false
}

func (s *Server) indexHandler(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, fmt.Sprintf("%s/Search/Simple.html?q=status:*", s.Prefix), http.StatusTemporaryRedirect)
}

var ticketTmpl = page.NewTemplate(
	"ticket",
	template.FuncMap{
		"obfuscateEmail": obfuscateEmail,
	},
	"web/templates/ticket.html")

func (s *Server) ticketHandler(w http.ResponseWriter, r *http.Request) {
	id := r.FormValue("id")
	d, err := s.Tix.GetTicket(id)
	if isNotFound(err) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		log.Printf("GetTicket(%v): %v", id, err)
		http.Error(w, "Internal Error", 500)
		return
	}

	p := s.NewPage("ticket", d)
	p.Render(w, ticketTmpl)
}

func (s *Server) attachHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	attID := vars["attachmentID"]

	filename, contentType, content, err := s.Tix.GetAttachment(attID)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	w.Header().Set("Content-Disposition",
		fmt.Sprintf("attachment; filename=%q", filename))
	w.Header().Set("Content-Type", contentType)
	w.Write(content)
}

var searchTmpl = page.NewTemplate(
	"search", template.FuncMap{
		"statusToBadgeClass": statusToBadgeClass,
	},
	"web/templates/search.html")

func (s *Server) searchHandler(w http.ResponseWriter, r *http.Request) {
	var d struct {
		Query      string
		Error      string
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
		Site       string
	}

	q := r.FormValue("q")
	d.Query = q
	d.Sizes = []int{10, 25, 50, 100}
	// TODO: These are available on the page object.
	d.Prefix = s.Prefix
	d.Site = s.Site

	if d.Query == "*" {
		d.Query = "status:*" // or we blow out the memory
	}

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
		order = "1" // Descending
	}

	if q != "" {

		sr := bleve.NewSearchRequestOptions(bleve.NewQueryStringQuery(q), int(pageSize), int(start), false)

		if order == "0" {
			sr.SortBy([]string{"id"})
		} else {
			sr.SortBy([]string{"-id"})
		}

		sr.Fields = []string{"id", "status", "subject"}

		searchResults, err := s.Tix.Index.SearchInContext(r.Context(), sr)
		if err != nil {
			d.Error = err.Error()
		}

		if searchResults != nil {
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
			d.Start = start + 1
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
	}

	p := s.NewPage("search", d)
	p.Render(w, searchTmpl)
}

func (s *Server) robotsTxtHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	// Disallow everything for now.
	w.Write([]byte(`User-agent: *
Disallow: /`))
}

// NewPage creates a new Page object and initializes the fields.
func (s *Server) NewPage(id string, c interface{}) *page.Page {
	p := page.New(id)
	p.Site = s.Site
	p.Prefix = s.Prefix
	p.GitHubPrefix = s.GitHubPrefix
	p.ShortSite = s.ShortSite
	p.Content = c
	return p
}

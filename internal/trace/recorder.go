package trace

import (
	"bytes"
	"context"
	"fmt"
	"fritzbook/internal/fbxml"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Recorder is an http.RoundTripper that records every exchange. Entries are stored
// raw and anonymized only in WriteTo, so secrets registered later are still covered.
type Recorder struct {
	next http.RoundTripper
	anon *Anonymizer

	mu      sync.Mutex
	entries []entry
	step    int
}

type entry struct {
	note string // set for free-text notes

	step       int
	method     string
	url        string
	reqHeader  http.Header
	reqBody    []byte
	status     string
	resHeader  http.Header
	resBody    []byte
	durationMS int64
	err        string
	omit       bool // omitted body: only Content-Type and Content-Length response headers are kept
	expect     *expectSpec
	truncate   bool // cap the anonymized response body at maxKeptHTML
}

// NewRecorder wraps next (http.DefaultTransport if nil).
func NewRecorder(next http.RoundTripper, a *Anonymizer) *Recorder {
	if next == nil {
		next = http.DefaultTransport
	}
	return &Recorder{next: next, anon: a}
}

// Note adds a free-text step note to the trace.
func (r *Recorder) Note(format string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = append(r.entries, entry{note: fmt.Sprintf(format, args...)})
}

// RoundTrip implements http.RoundTripper.
func (r *Recorder) RoundTrip(req *http.Request) (*http.Response, error) {
	e := entry{method: req.Method, url: req.URL.String(), reqHeader: req.Header.Clone()}
	omit := req.Context().Value(omitBodyKey{}) != nil
	keep := req.Context().Value(keepBodyKey{}) != nil
	e.expect, _ = req.Context().Value(expectKey{}).(*expectSpec)
	e.omit = omit
	first := req.Response == nil // redirect targets are never kept in full
	reqLen := 0
	if req.Body != nil {
		b, err := io.ReadAll(req.Body)
		_ = req.Body.Close()
		if err != nil {
			return nil, err
		}
		e.reqBody = b
		reqLen = len(b)
		req = req.Clone(req.Context())
		req.Body = io.NopCloser(bytes.NewReader(b))
	}
	start := time.Now()
	resp, err := r.next.RoundTrip(req)
	e.durationMS = time.Since(start).Milliseconds()
	if err != nil {
		e.err = err.Error()
		r.add(e)
		return nil, err
	}
	b, rerr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	e.status = resp.Status
	e.resHeader = resp.Header.Clone()
	e.resBody = b
	switch {
	case omit:
		e.reqBody = nil
		if reqLen > 0 {
			e.reqBody = []byte(fmt.Sprintf("[request body omitted: %d bytes]", reqLen))
		}
		e.resBody = []byte(fmt.Sprintf("[response body omitted: %d bytes]", len(b)))
	case isHTML(resp, b):
		if keep && first {
			e.truncate = true // cut after anonymization, in WriteTo
		} else {
			// HTML pages (login page, error pages) can embed user names and other free text.
			e.resBody = []byte(htmlPlaceholder(b))
		}
	}
	r.add(e)
	if rerr != nil {
		return nil, rerr
	}
	resp.Body = io.NopCloser(bytes.NewReader(b))
	return resp, nil
}

func (r *Recorder) add(e entry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.step++
	e.step = r.step
	r.entries = append(r.entries, e)
}

func (r *Recorder) writeHeaders(sb *strings.Builder, h http.Header) {
	names := make([]string, 0, len(h))
	for k := range h {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		for _, v := range h[k] {
			fmt.Fprintf(sb, "%s: %s\n", k, r.anon.Header(k, v))
		}
	}
}

// WriteTo writes the anonymized trace. It fails, writing nothing, if any registered
// secret still appears in the result.
func (r *Recorder) WriteTo(w io.Writer) (int64, error) {
	r.mu.Lock()
	entries := append([]entry(nil), r.entries...)
	r.mu.Unlock()

	var sb strings.Builder
	for _, e := range entries {
		if e.note != "" {
			fmt.Fprintf(&sb, "# %s\n\n", r.anon.Apply(e.note))
			continue
		}
		fmt.Fprintf(&sb, "=== Step %d: %s %s\n", e.step, e.method, r.anon.Apply(e.url))
		r.writeHeaders(&sb, e.reqHeader)
		if len(e.reqBody) > 0 {
			fmt.Fprintf(&sb, "\n%s\n", r.anon.Apply(string(e.reqBody)))
		}
		if e.err != "" {
			fmt.Fprintf(&sb, "--- error after %d ms: %s\n\n", e.durationMS, r.anon.Apply(e.err))
			continue
		}
		fmt.Fprintf(&sb, "--- %s (%d ms)\n", e.status, e.durationMS)
		resHeader := e.resHeader
		if e.omit {
			// Other headers (Content-Disposition with a file name, ...) can carry private names.
			resHeader = http.Header{}
			for _, k := range []string{"Content-Type", "Content-Length"} {
				if v := e.resHeader.Values(k); len(v) > 0 {
					resHeader[k] = v
				}
			}
		}
		r.writeHeaders(&sb, resHeader)
		resBody := e.resBody
		if e.expect != nil && !e.omit {
			if why := e.expect.violation(resBody); why != "" {
				fmt.Fprintf(&sb, "# WARNING: response of step %d is not the expected phonebook %q (%s); body omitted\n", e.step, e.expect.name, why)
				resBody = []byte(fmt.Sprintf("[response body omitted: %d bytes, %s]", len(e.resBody), why))
			}
		}
		if len(resBody) > 0 {
			body := r.anon.Apply(string(resBody))
			if e.truncate && len(body) > maxKeptHTML {
				body = body[:maxKeptHTML] + fmt.Sprintf("\n[truncated: %d of %d bytes kept]", maxKeptHTML, len(body))
			}
			fmt.Fprintf(&sb, "\n%s\n", body)
		}
		sb.WriteString("\n")
	}
	out := sb.String()

	r.anon.mu.Lock()
	secrets := append([]secret(nil), r.anon.secrets...)
	r.anon.mu.Unlock()
	for _, s := range secrets {
		if strings.Contains(out, s.value) {
			return 0, fmt.Errorf("trace: refusing to write, a registered %s secret survived anonymization", s.kind)
		}
	}
	n, err := io.WriteString(w, out)
	return int64(n), err
}

const maxKeptHTML = 64 << 10

type keepBodyKey struct{}

// KeepBody marks requests made with the returned context so that an HTML response body is
// recorded (first hop only, capped at 64 KB after anonymization) instead of being reduced to its title and length. The full
// anonymizer rules still apply when the trace is written.
func KeepBody(ctx context.Context) context.Context {
	return context.WithValue(ctx, keepBodyKey{}, true)
}

type omitBodyKey struct{}

// OmitBody marks requests made with the returned context so that neither the request nor the
// response body is recorded, only status, headers and sizes. Use it for private data.
func OmitBody(ctx context.Context) context.Context {
	return context.WithValue(ctx, omitBodyKey{}, true)
}

// isHTML detects HTML by content type or by sniffing the body.
func isHTML(resp *http.Response, body []byte) bool {
	ct := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Type")))
	if strings.HasPrefix(ct, "text/html") || strings.HasPrefix(ct, "application/xhtml") {
		return true
	}
	head := strings.ToLower(string(body[:min(len(body), 1024)]))
	return strings.HasPrefix(strings.TrimSpace(head), "<!doctype html") || strings.Contains(head, "<html")
}

var reTitle = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)

func htmlPlaceholder(b []byte) string {
	title := ""
	if m := reTitle.FindSubmatch(b); m != nil {
		title = strings.Join(strings.Fields(string(m[1])), " ")
	}
	return fmt.Sprintf("[HTML body omitted: %d bytes, title: %s]", len(b), title)
}

type expectKey struct{}

type expectSpec struct {
	name   string
	strict bool
}

// ExpectPhonebook marks requests whose response should be the phonebook called name. If the
// response parses as a phonebook with another name its body is omitted at WriteTo and a
// warning is written (the box may ignore the requested phonebook ID). With strict, a response
// that is not a phonebook at all is omitted too.
func ExpectPhonebook(ctx context.Context, name string, strict bool) context.Context {
	return context.WithValue(ctx, expectKey{}, &expectSpec{name, strict})
}

func (x *expectSpec) violation(body []byte) string {
	if !bytes.Contains(body, []byte("<phonebook")) && !bytes.Contains(body, []byte("<?xml")) {
		if x.strict && len(body) > 0 {
			return "not a phonebook"
		}
		return ""
	}
	info, err := fbxml.Inspect(bytes.NewReader(body))
	switch {
	case err != nil && (x.strict || bytes.Contains(body, []byte("<phonebook"))):
		return "not a phonebook"
	case err == nil && info.Name != x.name:
		return "other phonebook"
	}
	return ""
}

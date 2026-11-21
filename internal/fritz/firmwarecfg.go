package fritz

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
)

const maxRawBody = 64 << 20

// ExportPhonebookRaw requests the phonebook via cgi-bin/firmwarecfg and returns the
// status and body without interpreting them.
func (c *Client) ExportPhonebookRaw(ctx context.Context, id int, name string) (status int, body []byte, err error) {
	if c.SID == "" {
		return 0, nil, ErrNotLoggedIn
	}
	form := url.Values{
		"sid":                 {c.SID},
		"PhonebookId":         {strconv.Itoa(id)},
		"PhonebookExportName": {name},
		"PhonebookExport":     {""},
	}
	return c.firmwarecfg(ctx, "application/x-www-form-urlencoded", strings.NewReader(form.Encode()), false)
}

// ExportPhonebookMultipartRaw is ExportPhonebookRaw with a multipart/form-data body (fields
// sid, PhonebookId, PhonebookExportName, PhonebookExport in this order).
func (c *Client) ExportPhonebookMultipartRaw(ctx context.Context, id int, name string) (status int, body []byte, err error) {
	return c.exportMultipart(ctx, id, name, false)
}

func (c *Client) exportMultipart(ctx context.Context, id int, name string, noRedirect bool) (status int, body []byte, err error) {
	if c.SID == "" {
		return 0, nil, ErrNotLoggedIn
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for _, f := range [][2]string{{"sid", c.SID}, {"PhonebookId", strconv.Itoa(id)}, {"PhonebookExportName", name}, {"PhonebookExport", ""}} {
		if err := mw.WriteField(f[0], f[1]); err != nil {
			return 0, nil, err
		}
	}
	if err := mw.Close(); err != nil {
		return 0, nil, err
	}
	return c.firmwarecfg(ctx, mw.FormDataContentType(), &buf, noRedirect)
}

// ImportPhonebookRaw uploads xml into phonebook id via cgi-bin/firmwarecfg and returns the
// status and body without interpreting them.
func (c *Client) ImportPhonebookRaw(ctx context.Context, id int, xml []byte) (status int, body []byte, err error) {
	return c.importRaw(ctx, id, xml, false)
}

func (c *Client) importRaw(ctx context.Context, id int, xml []byte, noRedirect bool) (status int, body []byte, err error) {
	if c.SID == "" {
		return 0, nil, ErrNotLoggedIn
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField("sid", c.SID); err != nil {
		return 0, nil, err
	}
	if err := mw.WriteField("PhonebookId", strconv.Itoa(id)); err != nil {
		return 0, nil, err
	}
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", `form-data; name="PhonebookImportFile"; filename="fritzbook.xml"`)
	h.Set("Content-Type", "text/xml")
	part, err := mw.CreatePart(h)
	if err != nil {
		return 0, nil, err
	}
	if _, err := part.Write(xml); err != nil {
		return 0, nil, err
	}
	if err := mw.Close(); err != nil {
		return 0, nil, err
	}
	return c.firmwarecfg(ctx, mw.FormDataContentType(), &buf, noRedirect)
}

func (c *Client) firmwarecfg(ctx context.Context, contentType string, body io.Reader, noRedirect bool) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL.JoinPath("cgi-bin", "firmwarecfg").String(), body)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", contentType)
	hc := c.HTTP
	if noRedirect { // a redirect (to the login page) is the box's way of refusing
		cp := *c.HTTP
		cp.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		hc = &cp
	}
	res, err := c.doWith(hc, req)
	if err != nil {
		return 0, nil, fmt.Errorf("firmwarecfg: %w", err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, maxRawBody))
	if err != nil {
		return res.StatusCode, nil, fmt.Errorf("firmwarecfg: read response: %w", err)
	}
	return res.StatusCode, b, nil
}

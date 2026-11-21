// Package fritz talks to a FRITZ!Box: login, TR-064 and phonebook operations.
package fritz

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

const defaultBaseURL = "http://fritz.box"

// Client is a FRITZ!Box client. BaseURL serves the web interface, TR064URL the TR-064 services.
type Client struct {
	BaseURL  *url.URL
	TR064URL *url.URL
	HTTP     *http.Client
	SID      string

	user, password string
}

// New creates a client for baseURL (default "http://fritz.box"). The TR-064 URL uses the same
// host with port 49000 (http) or 49443 (https); tests override it.
func New(baseURL string, httpClient *http.Client) (*Client, error) {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse base URL: %w", err)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return nil, fmt.Errorf("invalid base URL %q: need http(s)://host", baseURL)
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	port := "49000"
	if u.Scheme == "https" {
		port = "49443"
	}
	tr := *u
	tr.Host = u.Hostname() + ":" + port
	if ip := u.Hostname(); strings.Contains(ip, ":") {
		tr.Host = "[" + ip + "]:" + port
	}
	return &Client{BaseURL: u, TR064URL: &tr, HTTP: httpClient}, nil
}

// do sends req. Transport errors are *url.Error values that embed the full request URL,
// which can carry the session ID; only the underlying cause is returned.
func (c *Client) do(req *http.Request) (*http.Response, error) {
	res, err := c.HTTP.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			return nil, ue.Err
		}
		return nil, err
	}
	return res, nil
}

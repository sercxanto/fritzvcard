package fritz

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

const (
	onTelControl  = "/upnp/control/x_contact"
	onTelService  = "urn:dslforum-org:service:X_AVM-DE_OnTel:1"
	deviceControl = "/upnp/control/deviceinfo"
	deviceService = "urn:dslforum-org:service:DeviceInfo:1"
)

// statusError carries the HTTP status of an unexpected TR-064 answer.
type statusError struct{ Status int }

func (e *statusError) Error() string { return fmt.Sprintf("HTTP status %d", e.Status) }

// ErrNotLoggedIn is returned by calls that need the login credentials.
var ErrNotLoggedIn = errors.New("not logged in")

// TR064 calls a TR-064 action with HTTP digest authentication, using the credentials of
// Login. It returns the output arguments of the response.
func (c *Client) TR064(ctx context.Context, controlURL, serviceType, action string, args map[string]string) (map[string]string, error) {
	if c.user == "" {
		return nil, ErrNotLoggedIn
	}
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="utf-8"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body>`)
	fmt.Fprintf(&sb, `<u:%s xmlns:u="%s">`, action, serviceType)
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		sb.WriteString("<" + k + ">")
		if err := xml.EscapeText(&sb, []byte(args[k])); err != nil {
			return nil, err
		}
		sb.WriteString("</" + k + ">")
	}
	fmt.Fprintf(&sb, `</u:%s></s:Body></s:Envelope>`, action)
	payload := []byte(sb.String())
	u := c.TR064URL.JoinPath(controlURL)
	u.Path = "/" + strings.TrimLeft(u.Path, "/") // JoinPath on a host-only URL yields no leading slash
	u.RawPath = ""

	send := func(auth string) (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", `text/xml; charset="utf-8"`)
		req.Header.Set("SOAPACTION", `"`+serviceType+"#"+action+`"`)
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		return c.do(req)
	}
	res, err := send("")
	if err != nil {
		return nil, fmt.Errorf("tr064 %s: %w", action, err)
	}
	if res.StatusCode == http.StatusUnauthorized {
		challenge := res.Header.Get("WWW-Authenticate")
		_, _ = io.Copy(io.Discard, res.Body)
		res.Body.Close()
		auth, err := digestAuthorization(challenge, c.user, c.password, http.MethodPost, u.RequestURI())
		if err != nil {
			return nil, fmt.Errorf("tr064 %s: %w", action, err)
		}
		if res, err = send(auth); err != nil {
			return nil, fmt.Errorf("tr064 %s: %w", action, err)
		}
		if res.StatusCode == http.StatusUnauthorized {
			_, _ = io.Copy(io.Discard, res.Body)
			res.Body.Close()
			return nil, fmt.Errorf("tr064 %s: %w", action, ErrBadCredentials)
		}
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 16<<20))
	if err != nil {
		return nil, fmt.Errorf("tr064 %s: read response: %w", action, err)
	}
	out, fault, perr := parseSOAP(body)
	switch {
	case fault != "":
		return nil, fmt.Errorf("tr064 %s: %s", action, fault)
	case res.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("tr064 %s: unexpected status %s: %w", action, res.Status, &statusError{res.StatusCode})
	case perr != nil:
		return nil, fmt.Errorf("tr064 %s: decode response: %w", action, perr)
	}
	return out, nil
}

// parseSOAP returns the output arguments of a response, or a fault description.
func parseSOAP(body []byte) (out map[string]string, fault string, err error) {
	dec := xml.NewDecoder(bytes.NewReader(body))
	out = map[string]string{}
	depth := 0
	var code, desc, cur string
	inFault, seenBody := false, false
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, "", err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			name := t.Name.Local
			switch {
			case name == "Fault":
				inFault = true
			case name == "Body":
				seenBody = true
			case inFault:
				cur = name
			case seenBody && depth == 4:
				cur = name
			}
		case xml.EndElement:
			depth--
			cur = ""
			if t.Name.Local == "Fault" {
				inFault = false
			}
		case xml.CharData:
			switch {
			case inFault && cur == "errorCode":
				code += string(t)
			case inFault && cur == "errorDescription":
				desc += string(t)
			case cur != "":
				out[cur] += string(t)
			}
		}
	}
	if code != "" || desc != "" {
		return nil, fmt.Sprintf("fault %s: %s", strings.TrimSpace(code), strings.TrimSpace(desc)), nil
	}
	if !seenBody {
		return nil, "", errors.New("no SOAP body")
	}
	return out, "", nil
}

// PhonebookIDsTR064 lists the phonebook IDs via X_AVM-DE_OnTel GetPhonebookList.
func (c *Client) PhonebookIDsTR064(ctx context.Context) ([]int, error) {
	out, err := c.TR064(ctx, onTelControl, onTelService, "GetPhonebookList", nil)
	if err != nil {
		return nil, err
	}
	var ids []int
	for _, f := range strings.Split(out["NewPhonebookList"], ",") {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		id, err := strconv.Atoi(f)
		if err != nil {
			return nil, fmt.Errorf("tr064 GetPhonebookList: invalid ID %q", f)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// PhonebookTR064 returns the name and download URL of phonebook id via GetPhonebook.
func (c *Client) PhonebookTR064(ctx context.Context, id int) (name, url string, err error) {
	out, err := c.TR064(ctx, onTelControl, onTelService, "GetPhonebook", map[string]string{"NewPhonebookID": strconv.Itoa(id)})
	if err != nil {
		return "", "", err
	}
	return out["NewPhonebookName"], out["NewPhonebookURL"], nil
}

// DeviceInfo returns the model name and FRITZ!OS version.
func (c *Client) DeviceInfo(ctx context.Context) (model, firmware string, err error) {
	out, err := c.TR064(ctx, deviceControl, deviceService, "GetInfo", nil)
	if err != nil {
		return "", "", err
	}
	return out["NewModelName"], out["NewSoftwareVersion"], nil
}

// Package trace records HTTP exchanges with a FRITZ!Box and anonymizes them so that
// the result can be shared safely.
package trace

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// Anonymizer replaces personal and secret data with stable placeholders.
type Anonymizer struct {
	keep string

	mu      sync.Mutex
	secrets []secret
	counts  map[string]map[string]int

	// disableRules turns every rule off. Only tests use it, to prove that the
	// leak check in Recorder.WriteTo works.
	disableRules bool
}

type secret struct{ value, kind string }

// NewAnonymizer returns an Anonymizer that leaves the phonebook named keepPhonebook
// untouched. An empty keepPhonebook keeps nothing.
func NewAnonymizer(keepPhonebook string) *Anonymizer {
	return &Anonymizer{keep: keepPhonebook, counts: map[string]map[string]int{}}
}

// AddSecret registers a value that must never appear in output. It is replaced by "<KIND>".
func (a *Anonymizer) AddSecret(value, kind string) {
	if value == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	seen := map[string]bool{}
	for _, v := range []string{value, url.QueryEscape(value), url.PathEscape(value), xmlEscape(value)} {
		if !seen[v] {
			seen[v] = true
			a.secrets = append(a.secrets, secret{v, kind})
		}
	}
}

var xmlEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")

func xmlEscape(s string) string { return xmlEscaper.Replace(s) }

var (
	reSIDParam   = regexp.MustCompile(`(?i)\bsid=[0-9A-Za-z]+`)
	reSIDTag     = regexp.MustCompile(`<SID>([^<]*)</SID>`)
	reChallenge  = regexp.MustCompile(`<Challenge>[^<]*</Challenge>`)
	reResponse   = regexp.MustCompile(`\bresponse=[^&\s"'<]+`)
	reUser       = regexp.MustCompile(`(<User(?:\s[^>]*)?>)([^<]+)(</User>)`)
	reDeviceLog  = regexp.MustCompile(`<(NewDeviceLog|NewProvisioningCode|NewDescription)>[^<]*</(NewDeviceLog|NewProvisioningCode|NewDescription)>`)
	reSerial     = regexp.MustCompile(`<(NewSerialNumber|Serial|serialNumber)>[^<]*</(NewSerialNumber|Serial|serialNumber)>`)
	reUDN        = regexp.MustCompile(`(<UDN>)([^<]+)(</UDN>)`)
	reMPSID      = regexp.MustCompile(`(?i)(name="sid"[^\r\n]*\r?\n(?:[A-Za-z-]+:[^\r\n]*\r?\n)*\r?\n)[^\r\n]+`)
	rePBURL      = regexp.MustCompile(`(<NewPhonebookURL>\s*[A-Za-z][A-Za-z0-9+.-]*://(?:[^@/<]*@)?)([^:/?#<\s]+)`)
	rePhonebook  = regexp.MustCompile(`(?s)<phonebook\b([^>]*)>(.*?)</phonebook>`)
	reNameAttr   = regexp.MustCompile(`\bname="([^"]*)"`)
	rePBField    = regexp.MustCompile(`<(realName|number|email|imageURL)(\s[^>]*)?>([^<]+)</(realName|number|email|imageURL)>`)
	reTRPBName   = regexp.MustCompile(`(<NewPhonebookName>)([^<]*)(</NewPhonebookName>)`)
	reMAC        = regexp.MustCompile(`\b(?:[0-9A-Fa-f]{2}[:-]){5}[0-9A-Fa-f]{2}\b`)
	reIPv4       = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	reIPv6       = regexp.MustCompile(`(?i)(?:[0-9a-f]{1,4}:){7}[0-9a-f]{1,4}|(?:[0-9a-f]{1,4}:){1,6}:(?:[0-9a-f]{1,4}(?::[0-9a-f]{1,4}){0,5})?|::[0-9a-f]{1,4}(?::[0-9a-f]{1,4}){0,6}`)
	reAuthParams = regexp.MustCompile(`(?i)\b(nonce|opaque)="[^"]*"`)
)

var fieldKinds = map[string]string{"realName": "NAME", "number": "NUMBER", "email": "EMAIL", "imageURL": "URL"}

func (a *Anonymizer) placeholder(kind, value string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	m := a.counts[kind]
	if m == nil {
		m = map[string]int{}
		a.counts[kind] = m
	}
	n, ok := m[value]
	if !ok {
		n = len(m) + 1
		m[value] = n
	}
	return fmt.Sprintf("<%s-%d>", kind, n)
}

func (a *Anonymizer) anonymizeName(name string) bool {
	return a.keep == "" || name != a.keep
}

// Apply anonymizes s.
func (a *Anonymizer) Apply(s string) string {
	if a.disableRules {
		return s
	}
	a.mu.Lock()
	secrets := append([]secret(nil), a.secrets...)
	a.mu.Unlock()
	sort.SliceStable(secrets, func(i, j int) bool { return len(secrets[i].value) > len(secrets[j].value) })
	for _, sec := range secrets {
		s = strings.ReplaceAll(s, sec.value, "<"+sec.kind+">")
	}

	s = reSIDParam.ReplaceAllStringFunc(s, func(m string) string { return m[:4] + "<SID>" })
	s = reSIDTag.ReplaceAllStringFunc(s, func(m string) string {
		c := reSIDTag.FindStringSubmatch(m)[1]
		if c == "" || strings.Trim(c, "0") == "" {
			return m
		}
		return "<SID><SID></SID>"
	})
	s = reMPSID.ReplaceAllString(s, "${1}<SID>")
	s = reChallenge.ReplaceAllString(s, "<Challenge><CHALLENGE></Challenge>")
	s = reResponse.ReplaceAllString(s, "response=<RESPONSE>")
	s = reUser.ReplaceAllStringFunc(s, func(m string) string {
		p := reUser.FindStringSubmatch(m)
		if strings.HasPrefix(p[2], "<") {
			return m
		}
		return p[1] + a.placeholder("USER", p[2]) + p[3]
	})
	s = reDeviceLog.ReplaceAllString(s, "<$1><REDACTED></$2>")
	s = reSerial.ReplaceAllString(s, "<$1><SERIAL></$2>")
	s = reUDN.ReplaceAllStringFunc(s, func(m string) string {
		p := reUDN.FindStringSubmatch(m)
		return p[1] + a.placeholder("UDN", p[2]) + p[3]
	})
	s = rePBURL.ReplaceAllStringFunc(s, func(m string) string {
		p := rePBURL.FindStringSubmatch(m)
		if reIPv4.MatchString(p[2]) && reIPv4.FindString(p[2]) == p[2] {
			return m // IPv4 hosts are handled by the IPv4 rule
		}
		return p[1] + a.placeholder("HOST", strings.ToLower(p[2]))
	})
	s = rePhonebook.ReplaceAllStringFunc(s, a.phonebook)
	s = reTRPBName.ReplaceAllStringFunc(s, func(m string) string {
		p := reTRPBName.FindStringSubmatch(m)
		if !a.anonymizeName(p[2]) {
			return m
		}
		return p[1] + a.placeholder("PHONEBOOK", p[2]) + p[3]
	})
	s = reMAC.ReplaceAllStringFunc(s, func(m string) string { return a.placeholder("MAC", strings.ToLower(strings.ReplaceAll(m, "-", ":"))) })
	s = reIPv6.ReplaceAllStringFunc(s, func(m string) string { return a.placeholder("IPv6", strings.ToLower(m)) })
	s = reIPv4.ReplaceAllStringFunc(s, func(m string) string { return a.placeholder("IPv4", m) })
	return s
}

func (a *Anonymizer) phonebook(block string) string {
	p := rePhonebook.FindStringSubmatch(block)
	attrs, body := p[1], p[2]
	nm := reNameAttr.FindStringSubmatch(attrs)
	name := ""
	if nm != nil {
		name = nm[1]
	}
	if !a.anonymizeName(name) {
		return block
	}
	if nm != nil {
		attrs = strings.Replace(attrs, nm[0], `name="`+a.placeholder("PHONEBOOK", name)+`"`, 1)
	}
	body = rePBField.ReplaceAllStringFunc(body, func(m string) string {
		f := rePBField.FindStringSubmatch(m)
		return "<" + f[1] + f[2] + ">" + a.placeholder(fieldKinds[f[1]], f[3]) + "</" + f[4] + ">"
	})
	return "<phonebook" + attrs + ">" + body + "</phonebook>"
}

// Header anonymizes a single header value; sensitive headers are redacted entirely.
func (a *Anonymizer) Header(name, value string) string {
	if a.disableRules {
		return value
	}
	switch http.CanonicalHeaderKey(name) {
	case "Authorization", "Cookie", "Set-Cookie":
		return "<REDACTED>"
	case "Www-Authenticate":
		value = reAuthParams.ReplaceAllString(value, `$1="<REDACTED>"`)
	}
	return a.Apply(value)
}

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
	guards  []string // names of other phonebooks, a best-effort backstop applied after the rules
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

// guardSkip lists words that appear in the trace's own structure or in rule placeholders.
// A guard equal to one of them is not registered: replacing it would garble the trace.
var guardSkip = map[string]bool{
	"user": true, "users": true, "username": true, "name": true, "number": true, "email": true,
	"phonebook": true, "phonebooks": true, "contact": true, "challenge": true, "step": true,
	"date": true, "sid": true, "host": true, "serial": true, "udn": true, "url": true, "uid": true,
	"ipv4": true, "ipv6": true, "mac": true, "redacted": true, "test": true, "error": true,
	"status": true, "server": true, "content": true, "type": true, "length": true,
	"response": true, "text": true, "value": true, "html": true, "xml": true, "note": true,
}

// AddGuard registers the name of a phonebook that must not appear in the trace. Unlike
// AddSecret it is applied AFTER the structural rules (so a name like "ser" cannot break the
// "<User>" rule) and is not part of the WriteTo leak check, so a name that also occurs in
// ordinary trace text can never make WriteTo refuse. Names shorter than 3 characters and
// structural words are ignored.
func (a *Anonymizer) AddGuard(name string) {
	if len(name) < 3 || guardSkip[strings.ToLower(name)] {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, v := range []string{name, url.QueryEscape(name), url.PathEscape(name), xmlEscape(name)} {
		a.guards = append(a.guards, v)
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
	reExportName = regexp.MustCompile(`\bPhonebookExportName=([^&\s"'<]*)`)
	reJSONKey    = regexp.MustCompile(`(?i)(?:\\"|"|&quot;)(?:activeUsers|users|username|user|name|UID|challenge)(?:\\"|"|&quot;)\s*:\s*`)
	reExportPH   = regexp.MustCompile(`^(?:%3C|<)PHONEBOOK-\d+(?:%3E|>)$`)
	reDeviceLog  = regexp.MustCompile(`<(NewDeviceLog|NewProvisioningCode|NewDescription|NewPhonebookExtraID)>[^<]*</(NewDeviceLog|NewProvisioningCode|NewDescription|NewPhonebookExtraID)>`)
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
	guards := append([]string(nil), a.guards...)
	a.mu.Unlock()
	sort.SliceStable(secrets, func(i, j int) bool { return len(secrets[i].value) > len(secrets[j].value) })
	// Credential secrets (user, password, SID) run first, before the structural rules.
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
	s = reExportName.ReplaceAllStringFunc(s, func(m string) string {
		v := reExportName.FindStringSubmatch(m)[1]
		name, err := url.QueryUnescape(v)
		if v == "" || (err == nil && !a.anonymizeName(name)) || reExportPH.MatchString(v) {
			return m
		}
		return "PhonebookExportName=" + url.QueryEscape(a.placeholder("PHONEBOOK", name))
	})
	s = redactJSON(s)
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
	// Phonebook-name guards run last, so they cannot break the rules above.
	sort.SliceStable(guards, func(i, j int) bool { return len(guards[i]) > len(guards[j]) })
	for _, g := range guards {
		s = strings.ReplaceAll(s, g, "<PHONEBOOK-NAME>")
	}
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

// quoteAt returns the length of a string-quote token at s[i:] (`"`, `&quot;` or the
// double-encoded `\"`), or 0.
func quoteAt(s string, i int) int {
	switch {
	case strings.HasPrefix(s[i:], "&quot;"):
		return len("&quot;")
	case strings.HasPrefix(s[i:], `\"`):
		return 2
	case s[i] == '"':
		return 1
	}
	return 0
}

// scanString returns the index after the string that opens at s[i] (a quote token), honoring
// backslash escapes. An unterminated string extends to the end.
func scanString(s string, i int) int {
	q := s[i : i+quoteAt(s, i)]
	for j := i + len(q); j < len(s); {
		if strings.HasPrefix(s[j:], q) {
			// A double-encoded string may contain escaped quotes (\\\"). Fail closed: its
			// closing quote must not follow a backslash and must precede a structural character.
			if q[0] != '\\' || (j > 0 && s[j-1] != '\\' && (j+2 >= len(s) || strings.IndexByte(",}] \t\r\n", s[j+2]) >= 0)) {
				return j + len(q)
			}
		}
		if s[j] == '\\' && j+1 < len(s) && q[0] != '\\' {
			j += 2
			continue
		}
		j++
	}
	return len(s)
}

// scanBracket returns the index after the array or object that opens at s[i], ignoring
// brackets inside strings. An unterminated value extends to the end.
func scanBracket(s string, i int, open, close byte) int {
	depth := 0
	for j := i; j < len(s); {
		if quoteAt(s, j) > 0 {
			j = scanString(s, j)
			continue
		}
		switch s[j] {
		case open:
			depth++
		case close:
			depth--
			if depth == 0 {
				return j + 1
			}
		}
		j++
	}
	return len(s)
}

// redactJSON removes values of JSON keys that carry user names or login material, in plain,
// HTML-escaped (&quot;) and double-encoded (\") form: array and object values are replaced
// entirely, string values by a placeholder. It runs in linear time.
func redactJSON(s string) string {
	var out strings.Builder
	pos := 0
	for _, m := range reJSONKey.FindAllStringIndex(s, -1) {
		if m[0] < pos || m[1] >= len(s) {
			continue
		}
		j := m[1]
		switch {
		case s[j] == '[':
			end := scanBracket(s, j, '[', ']')
			out.WriteString(s[pos:j] + `["<REDACTED>"]`)
			pos = end
		case s[j] == '{':
			end := scanBracket(s, j, '{', '}')
			out.WriteString(s[pos:j] + `{<REDACTED>}`)
			pos = end
		case quoteAt(s, j) > 0:
			end := scanString(s, j)
			q := s[j : j+quoteAt(s, j)]
			out.WriteString(s[pos:j] + q + "<REDACTED>" + q)
			pos = end
		}
	}
	out.WriteString(s[pos:])
	return out.String()
}

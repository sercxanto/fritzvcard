package fritz

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
)

// SolveDigest computes the RFC 2617 response (MD5; qop "auth", or legacy without qop).
func SolveDigest(user, password, realm, nonce, method, uri, qop, nc, cnonce string) string {
	md5hex := func(s string) string {
		sum := md5.Sum([]byte(s))
		return hex.EncodeToString(sum[:])
	}
	ha1 := md5hex(user + ":" + realm + ":" + password)
	ha2 := md5hex(method + ":" + uri)
	if qop == "" {
		return md5hex(ha1 + ":" + nonce + ":" + ha2)
	}
	return md5hex(ha1 + ":" + nonce + ":" + nc + ":" + cnonce + ":" + qop + ":" + ha2)
}

var reDigestParam = regexp.MustCompile(`(\w+)=(?:"([^"]*)"|([^,\s]+))`)

// digestAuthorization answers a "WWW-Authenticate: Digest ..." challenge.
func digestAuthorization(challenge, user, password, method, uri string) (string, error) {
	if len(challenge) < 7 || !strings.EqualFold(challenge[:7], "Digest ") {
		return "", fmt.Errorf("unsupported authentication challenge")
	}
	p := map[string]string{}
	for _, m := range reDigestParam.FindAllStringSubmatch(challenge[7:], -1) {
		p[strings.ToLower(m[1])] = m[2] + m[3]
	}
	if alg := p["algorithm"]; alg != "" && !strings.EqualFold(alg, "MD5") {
		return "", fmt.Errorf("unsupported digest algorithm %q", alg)
	}
	qop := ""
	for _, q := range strings.Split(p["qop"], ",") {
		if strings.TrimSpace(q) == "auth" {
			qop = "auth"
		}
	}
	if p["qop"] != "" && qop == "" {
		return "", fmt.Errorf("unsupported digest qop %q", p["qop"])
	}
	nc, cnonce := "00000001", ""
	if qop != "" {
		b := make([]byte, 8)
		if _, err := rand.Read(b); err != nil {
			return "", err
		}
		cnonce = hex.EncodeToString(b)
	}
	resp := SolveDigest(user, password, p["realm"], p["nonce"], method, uri, qop, nc, cnonce)
	h := fmt.Sprintf(`Digest username=%q, realm=%q, nonce=%q, uri=%q, algorithm=MD5, response=%q`,
		user, p["realm"], p["nonce"], uri, resp)
	if qop != "" {
		h += fmt.Sprintf(`, qop=auth, nc=%s, cnonce=%q`, nc, cnonce)
	}
	if o := p["opaque"]; o != "" {
		h += fmt.Sprintf(`, opaque=%q`, o)
	}
	return h, nil
}

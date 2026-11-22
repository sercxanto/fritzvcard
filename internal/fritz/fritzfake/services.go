package fritzfake

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"fritzbook/internal/fritz"
)

const (
	digestRealm = "F!Box SOAP-Auth"
	digestNonce = "fakenonce0123456789"
	soapNS      = `xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"`
)

var (
	reDigestField = regexp.MustCompile(`(\w+)=(?:"([^"]*)"|([^,\s]+))`)
	reSoapAction  = regexp.MustCompile(`^"?([^#"]+)#(\w+)"?$`)
	reArg         = regexp.MustCompile(`<(\w+)>([^<]*)</\w+>`)
)

func (b *Box) digestOK(r *http.Request) bool {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Digest ") {
		return false
	}
	f := map[string]string{}
	for _, m := range reDigestField.FindAllStringSubmatch(h[len("Digest "):], -1) {
		f[strings.ToLower(m[1])] = m[2] + m[3]
	}
	if f["username"] != b.User || f["realm"] != digestRealm || f["nonce"] != digestNonce || f["uri"] != r.URL.RequestURI() {
		return false
	}
	want := fritz.SolveDigest(b.User, b.Password, digestRealm, digestNonce, r.Method, f["uri"], f["qop"], f["nc"], f["cnonce"])
	return f["response"] == want
}

func (b *Box) handleTR064(w http.ResponseWriter, r *http.Request) {
	if b.TR064Status != 0 && r.URL.Path == "/upnp/control/x_contact" {
		if b.TR064Status == http.StatusUnauthorized {
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Digest realm=%q, nonce=%q, qop="auth", algorithm=MD5`, digestRealm, digestNonce))
		}
		http.Error(w, "refused", b.TR064Status)
		return
	}
	if b.NoTR064 && r.URL.Path == "/upnp/control/x_contact" {
		http.NotFound(w, r)
		return
	}
	if !b.digestOK(r) {
		w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Digest realm=%q, nonce=%q, qop="auth", algorithm=MD5`, digestRealm, digestNonce))
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	m := reSoapAction.FindStringSubmatch(r.Header.Get("SOAPACTION"))
	if m == nil {
		http.Error(w, "bad SOAPACTION", http.StatusBadRequest)
		return
	}
	service, action := m[1], m[2]
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	args := map[string]string{}
	for _, a := range reArg.FindAllStringSubmatch(string(body), -1) {
		args[a[1]] = a[2]
	}

	out := ""
	switch {
	case r.URL.Path == "/upnp/control/deviceinfo" && action == "GetInfo":
		model, fw := b.Model, b.Firmware
		if model == "" {
			model = "FRITZ!Box 7590"
		}
		if fw == "" {
			fw = "7.57"
		}
		out = "<NewModelName>" + model + "</NewModelName><NewSoftwareVersion>" + fw + "</NewSoftwareVersion>" +
			"<NewDescription>FRITZ!Box synthetic description</NewDescription><NewProvisioningCode>000.000.000.000</NewProvisioningCode>" +
			"<NewDeviceLog>01.01.26 10:00:00 WLAN device Johns-iPhone registered, user jdoe logged in, call to +49 30 5559999</NewDeviceLog>"
	case r.URL.Path == "/upnp/control/x_contact" && action == "GetPhonebookList":
		b.mu.Lock()
		ids := make([]string, 0, len(b.Phonebooks))
		for _, p := range b.Phonebooks {
			ids = append(ids, strconv.Itoa(p.ID))
		}
		b.mu.Unlock()
		out = "<NewPhonebookList>" + strings.Join(ids, ",") + "</NewPhonebookList>"
	case r.URL.Path == "/upnp/control/x_contact" && action == "GetPhonebook":
		id, _ := strconv.Atoi(args["NewPhonebookID"])
		if b.BrokenBookID != nil && *b.BrokenBookID == id {
			writeFault(w, 713, "Invalid array index")
			return
		}
		host := r.Host
		if b.PhonebookHost != "" {
			_, port, _ := net.SplitHostPort(r.Host)
			host = b.PhonebookHost + ":" + port
		}
		found := false
		b.mu.Lock()
		for _, p := range b.Phonebooks {
			if p.ID == id {
				found = true
				out = fmt.Sprintf("<NewPhonebookName>%s</NewPhonebookName><NewPhonebookURL>http://%s/phonebook.lua?pbid=%d&amp;sid=%s</NewPhonebookURL>", p.Name, host, id, validSID)
			}
		}
		b.mu.Unlock()
		if !found {
			writeFault(w, 713, "Invalid array index")
			return
		}
	default:
		writeFault(w, 401, "Invalid action")
		return
	}
	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	fmt.Fprintf(w, `<?xml version="1.0"?><s:Envelope %s><s:Body><u:%sResponse xmlns:u=%q>%s</u:%sResponse></s:Body></s:Envelope>`,
		soapNS, action, service, out, action)
}

func writeFault(w http.ResponseWriter, code int, desc string) {
	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	w.WriteHeader(http.StatusInternalServerError)
	fmt.Fprintf(w, `<?xml version="1.0"?><s:Envelope %s><s:Body><s:Fault><faultcode>s:Client</faultcode><faultstring>UPnPError</faultstring><detail><UPnPError xmlns="urn:dslforum-org:control-1-0"><errorCode>%d</errorCode><errorDescription>%s</errorDescription></UPnPError></detail></s:Fault></s:Body></s:Envelope>`,
		soapNS, code, desc)
}

// handleFirmwareCfg serves phonebook export (urlencoded form) and import (multipart).
// An import whose fields are not sid, PhonebookId, PhonebookImportFile in this order is
// answered with 400.
func (b *Box) handleFirmwareCfg(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		b.handleImport(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if r.PostForm.Get("sid") != validSID {
		http.Error(w, "invalid sid", http.StatusForbidden)
		return
	}
	if _, ok := r.PostForm["PhonebookExport"]; !ok {
		http.Error(w, "missing PhonebookExport", http.StatusBadRequest)
		return
	}
	id, _ := strconv.Atoi(r.PostForm.Get("PhonebookId"))
	// Like the real box, an export whose name does not match the phonebook's name is
	// bounced to the login page.
	if b.ExportBroken || b.phonebookName(id) != r.PostForm.Get("PhonebookExportName") || b.phonebookName(id) == "" {
		http.Redirect(w, r, "/index.lua", http.StatusFound)
		return
	}
	if b.ServeOtherID != nil {
		id = *b.ServeOtherID
	}
	if x := b.PhonebookXML(id); x != nil {
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="`+strings.ReplaceAll(b.phonebookName(id), " ", "_")+`.xml"`)
		w.Write(x)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	io.WriteString(w, "<html><body>no such phonebook</body></html>")
}

func (b *Box) handleImport(w http.ResponseWriter, r *http.Request) {
	mr, err := r.MultipartReader()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var names []string
	vals := map[string][]byte{}
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		data, _ := io.ReadAll(io.LimitReader(p, 1<<24))
		names = append(names, p.FormName())
		vals[p.FormName()] = data
		if p.FormName() == "PhonebookImportFile" && (p.FileName() != "fritzbook.xml" || p.Header.Get("Content-Type") != "text/xml") {
			http.Error(w, "bad import file part", http.StatusBadRequest)
			return
		}
	}
	if strings.Join(names, ",") == "sid,PhonebookId,PhonebookExportName,PhonebookExport" {
		b.handleMultipartExport(w, r, vals)
		return
	}
	if strings.Join(names, ",") != "sid,PhonebookId,PhonebookImportFile" {
		http.Error(w, "bad field order: "+strings.Join(names, ","), http.StatusBadRequest)
		return
	}
	if string(vals["sid"]) != validSID {
		http.Error(w, "invalid sid", http.StatusForbidden)
		return
	}
	if b.ImportRedirect {
		http.Redirect(w, r, "/index.lua", http.StatusFound)
		return
	}
	if b.ImportStatus != 0 {
		http.Error(w, "import refused", b.ImportStatus)
		return
	}
	if b.ImportDelay > 0 || b.HoldImport != nil {
		var delay <-chan time.Time
		if b.ImportDelay > 0 {
			delay = time.After(b.ImportDelay)
		}
		select {
		case <-delay:
		case <-b.HoldImport:
		case <-r.Context().Done():
			return
		}
		if b.ImportDrop {
			if hj, ok := w.(http.Hijacker); ok {
				if conn, _, err := hj.Hijack(); err == nil {
					conn.Close()
				}
			}
			return
		}
	}
	id, _ := strconv.Atoi(string(vals["PhonebookId"]))
	if b.ImportIgnored {
		b.mu.Lock()
		b.imports = append(b.imports, id)
		b.mu.Unlock()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		io.WriteString(w, "<html><body>Import successful</body></html>")
		return
	}
	if b.ImportTarget != nil {
		id = *b.ImportTarget
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := range b.Phonebooks {
		if b.Phonebooks[i].ID == id {
			if b.ImportAppends {
				b.Phonebooks[i].XML = appendContacts(b.Phonebooks[i].XML, vals["PhonebookImportFile"])
			} else {
				b.Phonebooks[i].XML = vals["PhonebookImportFile"]
			}
			b.imports = append(b.imports, id)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			if b.ImportAnswer != "" {
				io.WriteString(w, b.ImportAnswer)
				return
			}
			io.WriteString(w, "<html><body>Import successful</body></html>")
			return
		}
	}
	http.Error(w, "no such phonebook", http.StatusNotFound)
}

// appendContacts adds the <contact> elements of add before </phonebook> in base.
func appendContacts(base, add []byte) []byte {
	i, j, k := bytes.Index(add, []byte("<contact>")), bytes.LastIndex(add, []byte("</contact>")), bytes.LastIndex(base, []byte("</phonebook>"))
	if i < 0 || j < 0 || k < 0 {
		return base
	}
	out := append([]byte(nil), base[:k]...)
	out = append(out, add[i:j+len("</contact>")]...)
	return append(out, base[k:]...)
}

// handleMultipartExport answers the multipart variant of the export. Like the real box seems
// to, the fake rejects it with a redirect to the login page unless MultipartExportWorks is set.
func (b *Box) handleMultipartExport(w http.ResponseWriter, r *http.Request, vals map[string][]byte) {
	id, _ := strconv.Atoi(string(vals["PhonebookId"]))
	if !b.MultipartExportWorks || string(vals["sid"]) != validSID || b.phonebookName(id) != string(vals["PhonebookExportName"]) || b.phonebookName(id) == "" {
		http.Redirect(w, r, "/index.lua", http.StatusFound)
		return
	}
	if b.ServeOtherID != nil {
		id = *b.ServeOtherID
	}
	x := b.PhonebookXML(id)
	if x == nil {
		http.Redirect(w, r, "/index.lua", http.StatusFound)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="export_`+strings.ReplaceAll(b.phonebookName(id), " ", "_")+`.xml"`)
	w.Write(x)
}

var rePBOpen = regexp.MustCompile(`<phonebook\b`)

// renderPhonebookLua turns stored XML into the format of the real phonebook.lua download.
func (b *Box) renderPhonebookLua(id int, x []byte) []byte {
	s := string(x)
	n := strings.Count(s, "<contact>")
	s = strings.Replace(s, "<phonebooks>", fmt.Sprintf("<phonebooks><!-- id:%d , name: %s -->", id, b.phonebookName(id)), 1)
	if loc := rePBOpen.FindStringIndex(s); loc != nil {
		end := loc[1] + strings.Index(s[loc[1]:], ">") + 1
		s = s[:loc[0]] + `<phonebook owner="1"` + s[loc[0]+len("<phonebook"):end] + fmt.Sprintf("<timestamp>1700000000</timestamp><!-- number of contacts %d -->", n) + s[end:]
	}
	return []byte(s)
}

func (b *Box) phonebookName(id int) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, p := range b.Phonebooks {
		if p.ID == id {
			return p.Name
		}
	}
	return ""
}

// handleIndex serves a login page with synthetic user names embedded, like the real box.
func (b *Box) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	io.WriteString(w, `<!DOCTYPE html><html><head><title>FRITZ!Box</title></head><body><script>
var data = {"activeUsers":[{"value":"Maria Example","text":"Maria Example","UID":"boxuser1"},{"value":"Max Muster","text":"Max Muster","UID":"boxuser2"}],"username":"Maria Example","challenge":"2$10000$AABBCC$2000$DDEEFF"};
</script></body></html>`)
}

// handlePhonebookURL serves the download URL that GetPhonebook returns.
func (b *Box) handlePhonebookURL(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(r.URL.Query().Get("pbid"))
	if b.ServeOtherID != nil {
		id = *b.ServeOtherID
	}
	if b.LuaForbiddenID != nil && *b.LuaForbiddenID == id {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	x := b.PhonebookXML(id)
	if r.URL.Query().Get("sid") != validSID || x == nil {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	b.mu.Lock()
	b.fetches = append(b.fetches, id)
	b.mu.Unlock()
	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=pbook.xml")
	w.Write(b.renderPhonebookLua(id, x))
}

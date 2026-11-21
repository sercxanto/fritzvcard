package trace

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestApply_Secrets(t *testing.T) {
	a := NewAnonymizer("")
	a.AddSecret("s3cret-pw", "PASSWORD")
	a.AddSecret("alice", "USER")
	a.AddSecret("0123456789abcdef", "SID")
	in := "user=alice pw=s3cret-pw url=/x?sid=0123456789abcdef other=0123456789abcdef"
	out := a.Apply(in)
	for _, s := range []string{"s3cret-pw", "alice", "0123456789abcdef"} {
		if strings.Contains(out, s) {
			t.Errorf("output leaks %q: %s", s, out)
		}
	}
	if !strings.Contains(out, "<PASSWORD>") || !strings.Contains(out, "<USER>") {
		t.Errorf("missing placeholders: %s", out)
	}
}

func TestApply_ConsistentPlaceholders(t *testing.T) {
	a := NewAnonymizer("")
	out := a.Apply("a 00:11:22:33:44:55 b AA-BB-CC-DD-EE-FF c 00:11:22:33:44:55 ip 192.168.178.1 and 10.0.0.2 and 192.168.178.1 v6 fe80::1234:abcd")
	want := "a <MAC-1> b <MAC-2> c <MAC-1> ip <IPv4-1> and <IPv4-2> and <IPv4-1> v6 <IPv6-1>"
	if out != want {
		t.Errorf("got  %s\nwant %s", out, want)
	}
}

const exportXML = `<?xml version="1.0"?>
<phonebooks>
<phonebook name="Test"><contact><person><realName>Max Muster</realName></person><telephony><number type="home" prio="1">0301234567</number></telephony><services><email classifier="private">max@example.org</email></services></contact></phonebook>
<phonebook name="Privat"><contact><person><realName>Erika Mustermann</realName><imageURL>http://x.example/e.png</imageURL></person><telephony><number type="home" prio="1">0407654321</number></telephony><services><email classifier="private">erika@example.org</email></services></contact></phonebook>
</phonebooks>`

func TestApply_PhonebookKeepsTest(t *testing.T) {
	a := NewAnonymizer("Test")
	out := a.Apply(exportXML)
	for _, s := range []string{"Max Muster", "0301234567", "max@example.org", `name="Test"`} {
		if !strings.Contains(out, s) {
			t.Errorf("kept phonebook lost %q:\n%s", s, out)
		}
	}
	for _, s := range []string{"Privat", "Erika", "0407654321", "erika@example.org", "e.png"} {
		if strings.Contains(out, s) {
			t.Errorf("output leaks %q:\n%s", s, out)
		}
	}
	for _, s := range []string{`name="<PHONEBOOK-1>"`, "<NAME-1>", "<NUMBER-1>", "<EMAIL-1>", "<URL-1>"} {
		if !strings.Contains(out, s) {
			t.Errorf("missing %q:\n%s", s, out)
		}
	}
	tr := a.Apply("<NewPhonebookName>Privat</NewPhonebookName><NewPhonebookName>Test</NewPhonebookName>")
	if tr != "<NewPhonebookName><PHONEBOOK-1></NewPhonebookName><NewPhonebookName>Test</NewPhonebookName>" {
		t.Errorf("tr064 name: %s", tr)
	}
}

func TestApply_LoginXML(t *testing.T) {
	a := NewAnonymizer("")
	in := `<SessionInfo><SID>1a2b3c4d5e6f7a8b</SID><Challenge>deadbeef</Challenge><BlockTime>5</BlockTime><Users><User last="1">bob</User><User>carol</User></Users></SessionInfo>`
	out := a.Apply(in)
	for _, s := range []string{"1a2b3c4d5e6f7a8b", "deadbeef", "bob", "carol"} {
		if strings.Contains(out, s) {
			t.Errorf("leaks %q: %s", s, out)
		}
	}
	for _, s := range []string{"<SID><SID></SID>", "<Challenge><CHALLENGE></Challenge>", "<BlockTime>5</BlockTime>", `<User last="1"><USER-1></User>`, "<User><USER-2></User>"} {
		if !strings.Contains(out, s) {
			t.Errorf("missing %q: %s", s, out)
		}
	}
	zero := a.Apply("<SID>0000000000000000</SID>")
	if zero != "<SID>0000000000000000</SID>" {
		t.Errorf("zero SID changed: %s", zero)
	}
	if got := a.Apply("login?response=abc123$ff00&username=x&sid=ab12"); !strings.Contains(got, "response=<RESPONSE>") || !strings.Contains(got, "sid=<SID>") {
		t.Errorf("params: %s", got)
	}
	if got := a.Apply("<NewSerialNumber>ABC123</NewSerialNumber>"); got != "<NewSerialNumber><SERIAL></NewSerialNumber>" {
		t.Errorf("serial: %s", got)
	}
}

func TestRecorder_RecordsAndRedacts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Set-Cookie", "SID=topsecretcookie")
		w.WriteHeader(http.StatusTeapot)
		_, _ = io.WriteString(w, "hello")
	}))
	defer srv.Close()

	rec := NewRecorder(http.DefaultTransport, NewAnonymizer(""))
	rec.Note("step %d", 1)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/login", strings.NewReader("body-data"))
	req.Header.Set("Authorization", "Digest username=\"u\", response=\"zzz\"")
	resp, err := (&http.Client{Transport: rec}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(b) != "hello" {
		t.Fatalf("body not passed through: %q", b)
	}
	var buf bytes.Buffer
	if _, err := rec.WriteTo(&buf); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, s := range []string{"418", "POST", "body-data", "hello", "Authorization: <REDACTED>", "Set-Cookie: <REDACTED>", "step 1", "ms"} {
		if !strings.Contains(out, s) {
			t.Errorf("missing %q:\n%s", s, out)
		}
	}
	for _, s := range []string{"zzz", "topsecretcookie", "127.0.0.1"} {
		if strings.Contains(out, s) {
			t.Errorf("leaks %q:\n%s", s, out)
		}
	}
}

func TestWriteTo_FailsOnLeak(t *testing.T) {
	a := NewAnonymizer("")
	a.disableRules = true
	a.AddSecret("hunter2", "PASSWORD")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	rec := NewRecorder(http.DefaultTransport, a)
	resp, err := (&http.Client{Transport: rec}).Post(srv.URL, "text/plain", strings.NewReader("pw=hunter2"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	var buf bytes.Buffer
	n, err := rec.WriteTo(&buf)
	if err == nil {
		t.Fatal("expected leak error")
	}
	if n != 0 || buf.Len() != 0 {
		t.Errorf("wrote %d/%d bytes despite leak", n, buf.Len())
	}
	if strings.Contains(err.Error(), "hunter2") {
		t.Errorf("error message leaks secret: %v", err)
	}
}

func TestRecorder_SecretRegisteredLater(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "token latesecret99 here")
	}))
	defer srv.Close()
	a := NewAnonymizer("")
	rec := NewRecorder(http.DefaultTransport, a)
	resp, err := (&http.Client{Transport: rec}).Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	a.AddSecret("latesecret99", "SID")
	var buf bytes.Buffer
	if _, err := rec.WriteTo(&buf); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "latesecret99") || !strings.Contains(buf.String(), "<SID>") {
		t.Errorf("late secret not redacted:\n%s", buf.String())
	}
}

func TestAddSecret_EncodedVariants(t *testing.T) {
	const user = "a.b+c@example.test"
	const pw = "p&w <x>'\"y z"
	a := NewAnonymizer("")
	a.AddSecret(user, "USER")
	a.AddSecret(pw, "PASSWORD")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "<u>"+xmlEscape(user)+"</u><p>"+xmlEscape(pw)+"</p>")
	}))
	defer srv.Close()
	rec := NewRecorder(http.DefaultTransport, a)
	form := "username=" + url.QueryEscape(user) + "&password=" + url.QueryEscape(pw)
	resp, err := (&http.Client{Transport: rec}).Post(srv.URL+"/p/"+url.PathEscape(user)+"?u="+url.QueryEscape(user), "application/x-www-form-urlencoded", strings.NewReader(form))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	var buf bytes.Buffer
	if _, err := rec.WriteTo(&buf); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	out := buf.String()
	for _, s := range []string{user, url.QueryEscape(user), url.PathEscape(user), xmlEscape(user), pw, url.QueryEscape(pw), xmlEscape(pw)} {
		if strings.Contains(out, s) {
			t.Errorf("leaks %q:\n%s", s, out)
		}
	}
	// the leak check must see encoded variants too
	b := NewAnonymizer("")
	b.disableRules = true
	b.AddSecret(user, "USER")
	r2 := NewRecorder(http.DefaultTransport, b)
	resp, err = (&http.Client{Transport: r2}).Post(srv.URL, "text/plain", strings.NewReader("username="+url.QueryEscape(user)))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if _, err := r2.WriteTo(io.Discard); err == nil {
		t.Error("leak check missed encoded variant")
	}
}

func TestApply_MultipartSID(t *testing.T) {
	a := NewAnonymizer("")
	for _, nl := range []string{"\r\n", "\n"} {
		in := "--bnd" + nl + `Content-Disposition: form-data; name="sid"` + nl + nl + "0a1b2c3d4e5f6a7b" + nl +
			"--bnd" + nl + `Content-Disposition: form-data; name="PhonebookId"` + nl + nl + "0" + nl + "--bnd--"
		out := a.Apply(in)
		if strings.Contains(out, "0a1b2c3d4e5f6a7b") || !strings.Contains(out, nl+nl+"<SID>"+nl) {
			t.Errorf("multipart sid not redacted (%q): %q", nl, out)
		}
		if !strings.Contains(out, nl+nl+"0"+nl) {
			t.Errorf("other field damaged: %q", out)
		}
	}
}

func TestApply_DeviceDescriptionAndPhonebookURL(t *testing.T) {
	a := NewAnonymizer("")
	in := `<device><serialNumber>989BCB123456</serialNumber><UDN>uuid:12345678-0000-0000-0000-989BCB123456</UDN></device>` +
		`<NewPhonebookURL>https://x1y2.myfritz.net:49443/phonebook.lua?pb=0&amp;sid=abcd1234</NewPhonebookURL>` +
		`<NewPhonebookURL>http://192.168.1.1:49000/pb</NewPhonebookURL>`
	out := a.Apply(in)
	for _, s := range []string{"989BCB123456", "12345678-0000", "myfritz", "x1y2", "abcd1234", "192.168.1.1"} {
		if strings.Contains(out, s) {
			t.Errorf("leaks %q:\n%s", s, out)
		}
	}
	for _, s := range []string{"<serialNumber><SERIAL></serialNumber>", "<UDN><UDN-1></UDN>", "https://<HOST-1>:49443/phonebook.lua", "http://<IPv4-1>:49000/pb", "sid=<SID>"} {
		if !strings.Contains(out, s) {
			t.Errorf("missing %q:\n%s", s, out)
		}
	}
}

func TestApply_DeviceInfoLogRedacted(t *testing.T) {
	a := NewAnonymizer("Test")
	in := `<NewModelName>FRITZ!Box 7590</NewModelName><NewSoftwareVersion>7.57</NewSoftwareVersion>` +
		`<NewDescription>FRITZ!Box 7590 7.57 [x]</NewDescription><NewProvisioningCode>123.456.789.000</NewProvisioningCode>` +
		`<NewDeviceLog>WLAN device Johns-iPhone, user jdoe, call +49 30 5559999, host jdoe.myfritz.net</NewDeviceLog>`
	got := a.Apply(in)
	for _, leak := range []string{"Johns-iPhone", "jdoe", "5559999", "123.456.789.000", "[x]"} {
		if strings.Contains(got, leak) {
			t.Errorf("output leaks %q: %s", leak, got)
		}
	}
	for _, keep := range []string{"FRITZ!Box 7590</NewModelName>", "<NewSoftwareVersion>7.57</NewSoftwareVersion>"} {
		if !strings.Contains(got, keep) {
			t.Errorf("output lost %q: %s", keep, got)
		}
	}
}

func TestApply_LoginPageJSON(t *testing.T) {
	a := NewAnonymizer("Test")
	in := `var d = {"activeUsers":[{"value":"Maria Example","text":"Maria Example","UID":"boxuser1"},{"value":"Max Muster","text":"Max Muster","UID":"boxuser2"}],"username":"Maria Example","challenge":"2$10000$AABBCC$2000$DDEEFF","other":"keep"};`
	got := a.Apply(in)
	for _, leak := range []string{"Maria", "Max Muster", "boxuser", "AABBCC"} {
		if strings.Contains(got, leak) {
			t.Errorf("leaks %q: %s", leak, got)
		}
	}
	if !strings.Contains(got, `"other":"keep"`) {
		t.Errorf("over-redacted: %s", got)
	}
}

func TestApply_ExportNameForm(t *testing.T) {
	a := NewAnonymizer("Test")
	got := a.Apply("sid=<SID>&PhonebookId=0&PhonebookExportName=Privat%20B&PhonebookExport=")
	if strings.Contains(got, "Privat") {
		t.Errorf("leaks name: %s", got)
	}
	if got := a.Apply("PhonebookExportName=Test&x=1"); got != "PhonebookExportName=Test&x=1" {
		t.Errorf("Test name changed: %s", got)
	}
}

func TestRecorder_OmitsHTMLBodies(t *testing.T) {
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Status: "200 OK", Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}},
			Body: io.NopCloser(strings.NewReader("<html><head><title>My Box</title></head><body>Maria Example</body></html>"))}, nil
	})
	rec := NewRecorder(rt, NewAnonymizer("Test"))
	req, _ := http.NewRequest("GET", "http://box.example/index.lua", nil)
	res, err := rec.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	if !strings.Contains(string(body), "Maria Example") {
		t.Error("caller must still get the full body")
	}
	var sb strings.Builder
	if _, err := rec.WriteTo(&sb); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sb.String(), "Maria") || !strings.Contains(sb.String(), "[HTML body omitted: 73 bytes, title: My Box]") {
		t.Errorf("trace = %s", sb.String())
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestRecorder_KeepsHTMLBodyWhenOptedIn(t *testing.T) {
	page := "<html><head><title>Wait</title></head><body>Please confirm on your phone. " + strings.Repeat("x", 70000) + "</body></html>"
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Status: "200 OK", Header: http.Header{"Content-Type": {"text/html"}},
			Body: io.NopCloser(strings.NewReader(page))}, nil
	})
	rec := NewRecorder(rt, NewAnonymizer("Test"))
	for _, ctx := range []context.Context{context.Background(), KeepBody(context.Background())} {
		req, _ := http.NewRequestWithContext(ctx, "POST", "http://box.example/x", nil)
		if _, err := rec.RoundTrip(req); err != nil {
			t.Fatal(err)
		}
	}
	var sb strings.Builder
	if _, err := rec.WriteTo(&sb); err != nil {
		t.Fatal(err)
	}
	out := sb.String()
	if strings.Count(out, "Please confirm on your phone") != 1 {
		t.Errorf("want the text exactly once (opted-in request only)")
	}
	if !strings.Contains(out, "[HTML body omitted:") || !strings.Contains(out, "[truncated: 65536 of") {
		t.Errorf("missing omission or truncation marker")
	}
}

func TestApply_JSONKeysHardened(t *testing.T) {
	a := NewAnonymizer("Test")
	for name, in := range map[string]string{
		"bracket":  `{"activeUsers":[{"value":"Brack]et Name","text":"Brack]et Name","UID":"boxuser9"},{"value":"Second Person"}],"x":1}`,
		"escaped":  `{"activeUsers":[{"value":"Say \"hi]\" Person"}],"x":1}`,
		"htmlform": `{&quot;activeUsers&quot;:[{&quot;value&quot;:&quot;Quoted Person&quot;}],&quot;x&quot;:1}`,
		"others":   `{"user":"Quoted Person","name":"Name Person","users":["Users Person"],"UID":"boxuser7","username":"Quoted Person"}`,
	} {
		got := a.Apply(in)
		for _, leak := range []string{"Brack", "Second", "Person", "hi]", "boxuser", "Name"} {
			if strings.Contains(got, leak) {
				t.Errorf("%s: leaks %q: %s", name, leak, got)
			}
		}
	}
	if got := a.Apply(`{"activeUsers":["A]"],"x":1}`); !strings.HasSuffix(got, `,"x":1}`) {
		t.Errorf("over-redacted: %s", got)
	}
}

func TestApply_ExportNameAngleBracketNotExempt(t *testing.T) {
	a := NewAnonymizer("Test")
	got := a.Apply("PhonebookExportName=%3Cprivate%3E&x=1")
	if strings.Contains(got, "private") {
		t.Errorf("leaks: %s", got)
	}
}

func htmlRT(body string, ct string, redirect bool) roundTripFunc {
	return func(r *http.Request) (*http.Response, error) {
		h := http.Header{}
		if ct != "" {
			h.Set("Content-Type", ct)
		}
		return &http.Response{StatusCode: 200, Status: "200 OK", Header: h, Body: io.NopCloser(strings.NewReader(body))}, nil
	}
}

func recordOne(t *testing.T, rec *Recorder, req *http.Request) string {
	t.Helper()
	if _, err := rec.RoundTrip(req); err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	if _, err := rec.WriteTo(&sb); err != nil {
		t.Fatal(err)
	}
	return sb.String()
}

func TestRecorder_KeepBodyOnlyFirstHop(t *testing.T) {
	rec := NewRecorder(htmlRT("<html><title>Login</title>Maria Example</html>", "text/html", false), NewAnonymizer("Test"))
	req, _ := http.NewRequestWithContext(KeepBody(context.Background()), "GET", "http://box.example/index.lua", nil)
	req.Response = &http.Response{StatusCode: 302} // as set by http.Client on redirect targets
	out := recordOne(t, rec, req)
	if strings.Contains(out, "Maria") || !strings.Contains(out, "[HTML body omitted:") {
		t.Errorf("trace = %s", out)
	}
}

func TestRecorder_SniffsHTMLWithoutContentType(t *testing.T) {
	for _, ct := range []string{"", "text/plain", "application/xhtml+xml"} {
		rec := NewRecorder(htmlRT("\n  <!DOCTYPE html><html><title>T</title>Maria Example</html>", ct, false), NewAnonymizer("Test"))
		req, _ := http.NewRequest("GET", "http://box.example/x", nil)
		if out := recordOne(t, rec, req); strings.Contains(out, "Maria") {
			t.Errorf("content type %q: trace leaks: %s", ct, out)
		}
	}
}

func TestRecorder_OmitBody(t *testing.T) {
	rec := NewRecorder(htmlRT(`<phonebook name="Secret"><contact>Private Person</contact></phonebook>`, "text/xml", false), NewAnonymizer("Test"))
	req, _ := http.NewRequestWithContext(OmitBody(context.Background()), "POST", "http://box.example/x", strings.NewReader("PhonebookExportName=Secret"))
	out := recordOne(t, rec, req)
	if strings.Contains(out, "Secret") || strings.Contains(out, "Private") || !strings.Contains(out, "[response body omitted: 70 bytes]") || !strings.Contains(out, "[request body omitted: 26 bytes]") {
		t.Errorf("trace = %s", out)
	}
}

func TestRecorder_TruncatesAfterAnonymization(t *testing.T) {
	a := NewAnonymizer("Test")
	a.AddSecret("TOPSECRETVALUE", "S")
	body := "<html>" + strings.Repeat("x", maxKeptHTML-10) + "TOPSECRETVALUE" + strings.Repeat("y", 100) + "</html>"
	rec := NewRecorder(htmlRT(body, "text/html", false), a)
	req, _ := http.NewRequestWithContext(KeepBody(context.Background()), "GET", "http://box.example/x", nil)
	out := recordOne(t, rec, req) // would fail the leak check if a cut secret survived
	if !strings.Contains(out, "[truncated:") || strings.Contains(out, "TOPS") {
		t.Errorf("unexpected output (len %d)", len(out))
	}
}

func TestAddGuard_AfterRulesAndNoLeakCheck(t *testing.T) {
	a := NewAnonymizer("Test")
	a.AddGuard("ser")  // would break the <User> rule if applied first
	a.AddGuard("Step") // structural word: ignored
	got := a.Apply(`<User last="1">otheruser</User> in Hidden ser`)
	if strings.Contains(got, "otheruser") {
		t.Errorf("rule broken by guard: %s", got)
	}
	a.AddGuard("BOOK") // its own replacement "<PHONEBOOK-NAME>" contains it: a leak check would refuse
	rec := NewRecorder(htmlRT("<phonebooks></phonebooks>", "text/xml", false), a)
	req, _ := http.NewRequest("GET", "http://box.example/x?q=BOOK", nil)
	if out := recordOne(t, rec, req); !strings.Contains(out, "=== Step 1") {
		t.Errorf("structure garbled: %s", out)
	}
}

func TestAddGuard_ReplacesHeaderAndBody(t *testing.T) {
	a := NewAnonymizer("Test")
	a.AddGuard("Hidden Book")
	for _, in := range []string{`attachment; filename="Hidden Book.xml"`, "<html>Hidden%20Book here</html>", "Hidden Book"} {
		if got := a.Apply(in); strings.Contains(got, "Hidden") {
			t.Errorf("Apply(%q) = %q", in, got)
		}
	}
}

func TestRecorder_OmitBodyKeepsOnlyTypeAndLength(t *testing.T) {
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		h := http.Header{"Content-Type": {"text/xml"}, "Content-Length": {"2"}, "Content-Disposition": {`attachment; filename="Privat_Buero.xml"`}}
		return &http.Response{StatusCode: 200, Status: "200 OK", Header: h, Body: io.NopCloser(strings.NewReader("<a/>"))}, nil
	})
	rec := NewRecorder(rt, NewAnonymizer("Test"))
	req, _ := http.NewRequestWithContext(OmitBody(context.Background()), "GET", "http://box.example/x", nil)
	out := recordOne(t, rec, req)
	if strings.Contains(out, "Privat") || !strings.Contains(out, "Content-Type: text/xml") {
		t.Errorf("trace = %s", out)
	}
}

func TestRecorder_ExpectPhonebook(t *testing.T) {
	other := `<?xml version="1.0"?><phonebooks><phonebook name="Other"><contact><person><realName>Zed Person</realName></person><carddav_uid>zz-leak-marker</carddav_uid></contact></phonebook></phonebooks>`
	rec := NewRecorder(htmlRT(other, "text/xml", false), NewAnonymizer("Test"))
	req, _ := http.NewRequestWithContext(ExpectPhonebook(context.Background(), "Test", true), "GET", "http://box.example/x", nil)
	out := recordOne(t, rec, req)
	if strings.Contains(out, "zz-leak-marker") || !strings.Contains(out, "WARNING: response of step 1") {
		t.Errorf("trace = %s", out)
	}
	rec = NewRecorder(htmlRT("<phonebooks><phonebook name=\"Test\"></phonebook></phonebooks>", "text/xml", false), NewAnonymizer("Test"))
	req, _ = http.NewRequestWithContext(ExpectPhonebook(context.Background(), "Test", true), "GET", "http://box.example/x", nil)
	if out := recordOne(t, rec, req); strings.Contains(out, "WARNING") || !strings.Contains(out, `name="Test"`) {
		t.Errorf("Test book wrongly flagged: %s", out)
	}
}

func TestApply_RedactNewPhonebookExtraID(t *testing.T) {
	a := NewAnonymizer("Test")
	if got := a.Apply("<NewPhonebookExtraID>acct-123</NewPhonebookExtraID>"); strings.Contains(got, "acct") {
		t.Errorf("got %s", got)
	}
}

func TestApply_JSONObjectAndDoubleEncoded(t *testing.T) {
	a := NewAnonymizer("Test")
	for _, in := range []string{
		`{"users":{"u1":"Obj Person","n":{"x":"}"}},"k":1}`,
		`"{\"user\":\"Dbl Person\",\"k\":1}"`,
	} {
		if got := a.Apply(in); strings.Contains(got, "Person") {
			t.Errorf("leaks: %s", got)
		}
	}
	if got := a.Apply(`{"users":{"a":"}"},"k":1}`); !strings.HasSuffix(got, `,"k":1}`) {
		t.Errorf("over-redacted: %s", got)
	}
}

func TestApply_JSONLinearTime(t *testing.T) {
	a := NewAnonymizer("Test")
	in := strings.Repeat(`{"user":"x","users":[1,2`, 40000) // ~1 MB, unterminated
	start := time.Now()
	a.Apply(in)
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("Apply took %s", d)
	}
}

func TestApply_DoubleEncodedEscapedQuotes(t *testing.T) {
	a := NewAnonymizer("Test")
	in := `"{\"user\":\"Ann \\\"Q\\\" Person\",\"k\":1}"`
	got := a.Apply(in)
	if strings.Contains(got, "Person") || strings.Contains(got, "Ann") {
		t.Errorf("leaks: %s", got)
	}
	if !strings.Contains(got, `\"k\":1}`) {
		t.Errorf("over-redacted: %s", got)
	}
}

func TestRecorder_ExpectViolationDropsHeaders(t *testing.T) {
	other := `<phonebooks><phonebook name="Other"></phonebook></phonebooks>`
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		h := http.Header{"Content-Type": {"text/xml"}, "Content-Disposition": {`attachment; filename="Ab.xml"`}}
		return &http.Response{StatusCode: 200, Status: "200 OK", Header: h, Body: io.NopCloser(strings.NewReader(other))}, nil
	})
	rec := NewRecorder(rt, NewAnonymizer("Test"))
	req, _ := http.NewRequestWithContext(ExpectPhonebook(context.Background(), "Test", true), "GET", "http://box.example/x", nil)
	if out := recordOne(t, rec, req); strings.Contains(out, "Ab.xml") || !strings.Contains(out, "Content-Type: text/xml") {
		t.Errorf("trace = %s", out)
	}
}

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

package trace

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
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

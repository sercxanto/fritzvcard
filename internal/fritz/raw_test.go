package fritz_test

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"fritzbook/internal/fritz"
	"fritzbook/internal/fritz/fritzfake"
)

func newLoggedIn(t *testing.T, b *fritzfake.Box) *fritz.Client {
	t.Helper()
	c := newClient(t, b)
	tr := *c.BaseURL
	c.TR064URL = &tr // the fake serves both on one server
	if err := c.Login(context.Background(), b.User, b.Password); err != nil {
		t.Fatal(err)
	}
	return c
}

func testBox() *fritzfake.Box {
	return &fritzfake.Box{
		User: "alice", Password: "s3cret",
		Phonebooks: []fritzfake.Phonebook{
			{ID: 0, Name: "Privat", XML: []byte(`<phonebooks><phonebook name="Privat"><contact><person><realName>Secret</realName></person></contact></phonebook></phonebooks>`)},
			{ID: 1, Name: "Test", XML: []byte(`<phonebooks><phonebook name="Test"></phonebook></phonebooks>`)},
		},
	}
}

func TestSolveDigest_RFC2617(t *testing.T) {
	got := fritz.SolveDigest("Mufasa", "Circle Of Life", "testrealm@host.com",
		"dcd98b7102dd2f0e8b11d0f600bfb0c093", "GET", "/dir/index.html", "auth", "00000001", "0a4f113b")
	if want := "6629fae49393a05397450978507c4ef1"; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestTR064_GetPhonebookList(t *testing.T) {
	c := newLoggedIn(t, testBox())
	ids, err := c.PhonebookIDsTR064(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ids, []int{0, 1}) {
		t.Errorf("ids = %v", ids)
	}
	name, u, err := c.PhonebookTR064(context.Background(), 1)
	if err != nil || name != "Test" || u == "" {
		t.Errorf("got %q %q %v", name, u, err)
	}
	model, fw, err := c.DeviceInfo(context.Background())
	if err != nil || model == "" || fw == "" {
		t.Errorf("got %q %q %v", model, fw, err)
	}
}

func TestTR064_WrongPasswordNoLeak(t *testing.T) {
	b := testBox()
	c := newLoggedIn(t, b)
	b.Password = "changed-on-box" // box now rejects the digest
	_, err := c.PhonebookIDsTR064(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "s3cret") || strings.Contains(err.Error(), c.SID) {
		t.Errorf("error leaks a secret: %v", err)
	}
}

func TestTR064_RequiresLogin(t *testing.T) {
	c := newClient(t, testBox())
	if _, err := c.PhonebookIDsTR064(context.Background()); err == nil {
		t.Fatal("expected error without login")
	}
}

func TestExportImportRaw(t *testing.T) {
	b := testBox()
	c := newLoggedIn(t, b)
	status, body, err := c.ExportPhonebookRaw(context.Background(), 0, "Privat")
	if err != nil || status != 200 || !strings.Contains(string(body), `name="Privat"`) {
		t.Fatalf("export: %d %q %v", status, body, err)
	}
	status, body, err = c.ExportPhonebookRaw(context.Background(), 7, "")
	if err != nil || strings.Contains(string(body), "<phonebook") {
		t.Fatalf("export of unknown id: %d %q %v", status, body, err)
	}
	xml := []byte(`<phonebooks><phonebook name="Test"><contact><person><realName>Probe Contact 1</realName></person></contact></phonebook></phonebooks>`)
	status, _, err = c.ImportPhonebookRaw(context.Background(), 1, xml)
	if err != nil || status != 200 {
		t.Fatalf("import: %d %v", status, err)
	}
	if got := b.Imports(); !reflect.DeepEqual(got, []int{1}) { // fake rejects wrong field order with 400
		t.Errorf("imports = %v", got)
	}
	if string(b.PhonebookXML(1)) != string(xml) {
		t.Errorf("stored xml = %q", b.PhonebookXML(1))
	}
}

func TestExportRaw_WrongNameBouncesToLoginPage(t *testing.T) {
	b := testBox()
	c := newLoggedIn(t, b)
	_, body, err := c.ExportPhonebookRaw(context.Background(), 0, "")
	if err != nil || strings.Contains(string(body), "<phonebook") {
		t.Fatalf("got %q %v", body, err)
	}
	reqs := b.Requests()
	if reqs[len(reqs)-1] != "GET /index.lua" {
		t.Errorf("requests = %v", reqs)
	}
}

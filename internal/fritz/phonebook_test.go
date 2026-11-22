package fritz_test

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"fritzbook/internal/fbxml"
	"fritzbook/internal/fritz"
	"fritzbook/internal/fritz/fritzfake"
)

const importXML = `<?xml version="1.0" encoding="utf-8"?><phonebooks><phonebook name="Test"><contact><person><realName>Probe Contact 1</realName></person></contact><contact><person><realName>Probe Contact 2</realName></person></contact></phonebook></phonebooks>`

func TestListPhonebooks(t *testing.T) {
	c := newLoggedIn(t, testBox())
	got, err := c.ListPhonebooks(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []fritz.Phonebook{{ID: 0, Name: "Privat"}, {ID: 1, Name: "Test"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestListPhonebooks_TR064Disabled(t *testing.T) {
	tests := []struct {
		name  string
		setup func(b *fritzfake.Box, c *fritz.Client)
	}{
		{"forbidden", func(b *fritzfake.Box, _ *fritz.Client) { b.TR064Status = http.StatusForbidden }},
		{"unauthorized", func(b *fritzfake.Box, _ *fritz.Client) { b.TR064Status = http.StatusUnauthorized }},
		{"not found", func(b *fritzfake.Box, _ *fritz.Client) { b.NoTR064 = true }},
		{"unreachable", func(_ *fritzfake.Box, c *fritz.Client) {
			tr := *c.TR064URL
			tr.Host = "127.0.0.1:1"
			c.TR064URL = &tr
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := testBox()
			c := newLoggedIn(t, b)
			tt.setup(b, c)
			_, err := c.ListPhonebooks(context.Background())
			if err == nil {
				t.Fatal("expected error")
			}
			for _, s := range []string{"Allow access for applications", "permissions"} {
				if !strings.Contains(err.Error(), s) {
					t.Errorf("error %q lacks %q", err, s)
				}
			}
			if strings.Contains(err.Error(), "s3cret") || strings.Contains(err.Error(), c.SID) {
				t.Errorf("error leaks a secret: %v", err)
			}
		})
	}
}

func TestFindPhonebook(t *testing.T) {
	b := testBox()
	b.Phonebooks = append(b.Phonebooks, fritzfake.Phonebook{ID: 2, Name: "Dup", XML: []byte("<phonebooks/>")}, fritzfake.Phonebook{ID: 3, Name: "Dup", XML: []byte("<phonebooks/>")})
	c := newLoggedIn(t, b)
	tests := []struct {
		name    string
		want    fritz.Phonebook
		wantErr error
		msg     string
	}{
		{name: "Test", want: fritz.Phonebook{ID: 1, Name: "Test"}},
		{name: "test", wantErr: fritz.ErrPhonebookNotFound, msg: `phonebook "test" not found — create it in the FRITZ!Box web UI first`},
		{name: "Missing", wantErr: fritz.ErrPhonebookNotFound, msg: `phonebook "Missing" not found — create it in the FRITZ!Box web UI first`},
		{name: "Dup", wantErr: fritz.ErrAmbiguousPhonebook, msg: `several phonebooks are named "Dup"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := c.FindPhonebook(context.Background(), tt.name)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil && err.Error() != tt.msg {
				t.Errorf("err %q, want exactly %q", err, tt.msg)
			}
			if got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExportPhonebook(t *testing.T) {
	b := testBox()
	b.MultipartExportWorks = true
	c := newLoggedIn(t, b)
	x, err := c.ExportPhonebook(context.Background(), 1, "Test")
	if err != nil {
		t.Fatal(err)
	}
	info, err := fbxml.Inspect(strings.NewReader(string(x)))
	if err != nil || info.Name != "Test" {
		t.Errorf("info %v, err %v", info, err)
	}
}

func TestExportPhonebook_Errors(t *testing.T) {
	tests := []struct {
		name string
		id   int
		pb   string
		set  func(b *fritzfake.Box)
	}{
		{"bounced to login page", 1, "Test", func(b *fritzfake.Box) { b.MultipartExportWorks = false }},
		{"wrong name", 1, "Wrong", nil},
		{"unknown id", 9, "Test", nil},
		{"box ignores the ID and serves another book", 1, "Test", func(b *fritzfake.Box) { zero := 0; b.ServeOtherID = &zero }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := testBox()
			b.MultipartExportWorks = true
			if tt.set != nil {
				tt.set(b)
			}
			c := newLoggedIn(t, b)
			x, err := c.ExportPhonebook(context.Background(), tt.id, tt.pb)
			if err == nil {
				t.Fatalf("expected error, got %q", x)
			}
			if strings.Contains(err.Error(), c.SID) {
				t.Errorf("error leaks SID: %v", err)
			}
		})
	}
}

func TestExportPhonebook_WrongBook(t *testing.T) {
	b := testBox()
	b.MultipartExportWorks = true
	zero := 0
	b.ServeOtherID = &zero
	c := newLoggedIn(t, b)
	x, err := c.ExportPhonebook(context.Background(), 1, "Test")
	if !errors.Is(err, fritz.ErrUnexpectedPhonebook) {
		t.Fatalf("err = %v, body %q", err, x)
	}
	if !strings.Contains(err.Error(), `"Test"`) || !strings.Contains(err.Error(), `"Privat"`) {
		t.Errorf("error does not name both books: %v", err)
	}
}

func TestImport_Success(t *testing.T) {
	b := testBox()
	b.ImportAnswer = "<html>Fehler: das ist lokalisiert</html>" // the answer text never decides
	c := newLoggedIn(t, b)
	var called atomic.Int32
	if err := c.ImportPhonebook(context.Background(), 1, []byte(importXML), func() { called.Add(1) }); err != nil {
		t.Fatal(err)
	}
	if called.Load() != 0 {
		t.Error("onWait called for a fast import")
	}
	if got := b.Imports(); !reflect.DeepEqual(got, []int{1}) {
		t.Errorf("imports = %v", got)
	}
}

func TestImport_NilCallback(t *testing.T) {
	b := testBox()
	b.ImportDelay = 60 * time.Millisecond
	c := newLoggedIn(t, b)
	c.WaitHintAfter = 10 * time.Millisecond
	if err := c.ImportPhonebook(context.Background(), 1, []byte(importXML), nil); err != nil {
		t.Fatal(err)
	}
}

func TestImport_SlowSuccessHintsOnce(t *testing.T) {
	b := testBox()
	b.HoldImport = make(chan struct{})
	c := newLoggedIn(t, b)
	c.WaitHintAfter = 30 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var calls atomic.Int32
	err := c.ImportPhonebook(ctx, 1, []byte(importXML), func() {
		if calls.Add(1) == 1 {
			close(b.HoldImport)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Errorf("onWait called %d times, want 1", calls.Load())
	}
	if got := b.Imports(); !reflect.DeepEqual(got, []int{1}) {
		t.Errorf("imports = %v", got)
	}
}

func TestImport_DeadlineAfterHint(t *testing.T) {
	b := testBox()
	b.HoldImport = make(chan struct{})
	t.Cleanup(func() { close(b.HoldImport) })
	c := newLoggedIn(t, b)
	c.WaitHintAfter = 20 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	var calls atomic.Int32
	err := c.ImportPhonebook(ctx, 1, []byte(importXML), func() { calls.Add(1) })
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
	if calls.Load() != 1 {
		t.Errorf("onWait called %d times", calls.Load())
	}
	if strings.Contains(err.Error(), c.SID) {
		t.Errorf("error leaks SID: %v", err)
	}
}

func TestImport_DeadlineBeforeHint(t *testing.T) {
	b := testBox()
	b.HoldImport = make(chan struct{})
	t.Cleanup(func() { close(b.HoldImport) })
	c := newLoggedIn(t, b)
	c.WaitHintAfter = 5 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	err := c.ImportPhonebook(ctx, 1, []byte(importXML), func() { t.Error("unexpected wait hint") })
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want plain deadline error", err)
	}
}

func TestImport_CancelledAfterHint(t *testing.T) {
	b := testBox()
	b.HoldImport = make(chan struct{})
	t.Cleanup(func() { close(b.HoldImport) })
	c := newLoggedIn(t, b)
	c.WaitHintAfter = 20 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := c.ImportPhonebook(ctx, 1, []byte(importXML), cancel)
	if !errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want cancellation", err)
	}
}

func TestImport_Rejected(t *testing.T) {
	tests := []struct {
		name   string
		set    func(b *fritzfake.Box)
		status int
	}{
		{"redirect to login", func(b *fritzfake.Box) { b.ImportRedirect = true }, http.StatusFound},
		{"server error", func(b *fritzfake.Box) { b.ImportStatus = http.StatusInternalServerError }, http.StatusInternalServerError},
		{"forbidden", func(b *fritzfake.Box) { b.ImportStatus = http.StatusForbidden }, http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := testBox()
			tt.set(b)
			c := newLoggedIn(t, b)
			err := c.ImportPhonebook(context.Background(), 1, []byte(importXML), nil)
			var ie *fritz.ImportError
			if !errors.As(err, &ie) {
				t.Fatalf("err = %v, want *ImportError", err)
			}
			if ie.Status != tt.status || ie.Message == "" {
				t.Errorf("got %+v", ie)
			}
			if strings.Contains(err.Error(), c.SID) {
				t.Errorf("error leaks SID: %v", err)
			}
			if len(b.Imports()) != 0 {
				t.Errorf("imports = %v", b.Imports())
			}
		})
	}
}

func TestImport_RequiresLogin(t *testing.T) {
	c := newClient(t, testBox())
	if err := c.ImportPhonebook(context.Background(), 1, []byte(importXML), nil); !errors.Is(err, fritz.ErrNotLoggedIn) {
		t.Errorf("err = %v", err)
	}
}

func TestListPhonebooks_OtherFailuresHaveNoHint(t *testing.T) {
	b := testBox()
	c := newLoggedIn(t, b)
	b.TR064Status = http.StatusInternalServerError
	_, err := c.ListPhonebooks(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "Allow access for applications") {
		t.Errorf("hint on a 500: %v", err)
	}
}

func TestImport_ConnectionDropIsNotTimeout(t *testing.T) {
	b := testBox()
	b.ImportDelay = 80 * time.Millisecond
	b.ImportDrop = true
	c := newLoggedIn(t, b)
	c.WaitHintAfter = 20 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var calls atomic.Int32
	err := c.ImportPhonebook(ctx, 1, []byte(importXML), func() { calls.Add(1) })
	if err == nil || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want the plain connection error", err)
	}
	if calls.Load() != 1 {
		t.Errorf("onWait called %d times", calls.Load())
	}
}

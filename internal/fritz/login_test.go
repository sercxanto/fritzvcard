package fritz_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"fritzvcard/internal/fritz"
	"fritzvcard/internal/fritz/fritzfake"
)

func newClient(t *testing.T, b *fritzfake.Box) *fritz.Client {
	t.Helper()
	srv := b.Start(t)
	c, err := fritz.New(srv.URL, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestSolveChallenge_AVMVector(t *testing.T) {
	got, err := fritz.SolveChallenge("2$10000$5A1711$2000$5A1722", "1example!")
	if err != nil {
		t.Fatal(err)
	}
	want := "5A1722$1798a1672bca7c6463d6b245f82b53703b0f50813401b03e4045a5861e689adb"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSolveChallenge_Invalid(t *testing.T) {
	for _, ch := range []string{"", "1$a$b", "2$x$5A$2000$5B", "2$10$zz$2000$5B", "2$10$5A$2000"} {
		if _, err := fritz.SolveChallenge(ch, "pw"); err == nil {
			t.Errorf("SolveChallenge(%q): expected error", ch)
		}
	}
}

func TestNew_Defaults(t *testing.T) {
	c, err := fritz.New("", nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.BaseURL.String() != "http://fritz.box" || c.TR064URL.String() != "http://fritz.box:49000" {
		t.Errorf("got %v / %v", c.BaseURL, c.TR064URL)
	}
	c, err = fritz.New("https://192.0.2.1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.TR064URL.String() != "https://192.0.2.1:49443" {
		t.Errorf("got %v", c.TR064URL)
	}
}

func TestLogin_Success(t *testing.T) {
	b := &fritzfake.Box{User: "alice", Password: "s3cret"}
	c := newClient(t, b)
	if err := c.Login(context.Background(), "alice", "s3cret"); err != nil {
		t.Fatal(err)
	}
	if c.SID == "" || c.SID == "0000000000000000" {
		t.Errorf("SID = %q", c.SID)
	}
	want := []string{"GET /login_sid.lua", "POST /login_sid.lua"}
	if got := b.Requests(); len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("requests = %v, want %v", got, want)
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	c := newClient(t, &fritzfake.Box{User: "alice", Password: "s3cret"})
	err := c.Login(context.Background(), "alice", "nope")
	if !errors.Is(err, fritz.ErrBadCredentials) {
		t.Fatalf("err = %v", err)
	}
	if c.SID != "" {
		t.Errorf("SID = %q, want empty", c.SID)
	}
}

func TestLogin_Blocked(t *testing.T) {
	c := newClient(t, &fritzfake.Box{User: "alice", Password: "s3cret", BlockTime: 7})
	err := c.Login(context.Background(), "alice", "s3cret")
	var be *fritz.BlockedError
	if !errors.As(err, &be) || be.Seconds != 7 {
		t.Fatalf("err = %v", err)
	}
	if be.Error() != "login blocked by the FRITZ!Box for 7 seconds" {
		t.Errorf("msg = %q", be.Error())
	}
}

func TestLogin_OldFirmware(t *testing.T) {
	c := newClient(t, &fritzfake.Box{User: "alice", Password: "s3cret", OldFirmware: true})
	if err := c.Login(context.Background(), "alice", "s3cret"); !errors.Is(err, fritz.ErrOldFirmware) {
		t.Fatalf("err = %v", err)
	}
}

func TestLogout(t *testing.T) {
	b := &fritzfake.Box{User: "alice", Password: "s3cret"}
	c := newClient(t, b)
	if err := c.Login(context.Background(), "alice", "s3cret"); err != nil {
		t.Fatal(err)
	}
	if err := c.Logout(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c.SID != "" {
		t.Errorf("SID = %q, want empty", c.SID)
	}
	reqs := b.Requests()
	if got := reqs[len(reqs)-1]; got != "GET /login_sid.lua" {
		t.Errorf("last request = %q", got)
	}
	if !b.WasLoggedOut() {
		t.Error("box did not see logout")
	}
}

func TestLogout_NoSession(t *testing.T) {
	b := &fritzfake.Box{}
	c := newClient(t, b)
	if err := c.Logout(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := b.Requests(); len(got) != 0 {
		t.Errorf("requests = %v", got)
	}
}

func TestLogout_ErrorDoesNotLeakSID(t *testing.T) {
	b := &fritzfake.Box{User: "alice", Password: "s3cret"}
	srv := b.Start(t)
	c, err := fritz.New(srv.URL, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Login(context.Background(), "alice", "s3cret"); err != nil {
		t.Fatal(err)
	}
	sid := c.SID
	srv.Close()
	err = c.Logout(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), sid) || strings.Contains(err.Error(), "sid=") {
		t.Errorf("error leaks the SID: %v", err)
	}
}

package fritz

import (
	"context"
	"crypto/pbkdf2"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const emptySID = "0000000000000000"

// Errors returned by Login.
var (
	ErrBadCredentials = errors.New("login failed: wrong user name or password")
	ErrOldFirmware    = errors.New("FRITZ!OS 7.24 or newer required")
)

// BlockedError reports that the box refuses logins for a while.
type BlockedError struct{ Seconds int }

func (e *BlockedError) Error() string {
	return fmt.Sprintf("login blocked by the FRITZ!Box for %d seconds", e.Seconds)
}

type sessionInfo struct {
	SID       string `xml:"SID"`
	Challenge string `xml:"Challenge"`
	BlockTime int    `xml:"BlockTime"`
}

// SolveChallenge computes the PBKDF2 login response for a "2$iter1$salt1$iter2$salt2" challenge.
func SolveChallenge(challenge, password string) (string, error) {
	p := strings.Split(challenge, "$")
	if len(p) != 5 || p[0] != "2" {
		return "", fmt.Errorf("unsupported challenge format")
	}
	iter1, err1 := strconv.Atoi(p[1])
	iter2, err2 := strconv.Atoi(p[3])
	if err1 != nil || err2 != nil || iter1 <= 0 || iter2 <= 0 {
		return "", fmt.Errorf("invalid iteration count in challenge")
	}
	salt1, err1 := hex.DecodeString(p[2])
	salt2, err2 := hex.DecodeString(p[4])
	if err1 != nil || err2 != nil {
		return "", fmt.Errorf("invalid salt in challenge")
	}
	h1, err := pbkdf2.Key(sha256.New, password, salt1, iter1, 32)
	if err != nil {
		return "", err
	}
	h2, err := pbkdf2.Key(sha256.New, string(h1), salt2, iter2, 32)
	if err != nil {
		return "", err
	}
	return p[4] + "$" + hex.EncodeToString(h2), nil
}

// Login authenticates and stores the session ID in c.SID.
func (c *Client) Login(ctx context.Context, user, password string) error {
	loginURL := c.BaseURL.JoinPath("login_sid.lua")
	loginURL.RawQuery = "version=2"

	info, err := c.sessionInfo(ctx, http.MethodGet, loginURL.String(), nil)
	if err != nil {
		return err
	}
	if info.BlockTime > 0 {
		return &BlockedError{Seconds: info.BlockTime}
	}
	if !strings.HasPrefix(info.Challenge, "2$") {
		return ErrOldFirmware
	}
	resp, err := SolveChallenge(info.Challenge, password)
	if err != nil {
		return err
	}
	form := url.Values{"username": {user}, "response": {resp}}
	info, err = c.sessionInfo(ctx, http.MethodPost, loginURL.String(), strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	if info.BlockTime > 0 {
		return &BlockedError{Seconds: info.BlockTime}
	}
	if info.SID == "" || info.SID == emptySID {
		return ErrBadCredentials
	}
	c.SID, c.user, c.password = info.SID, user, password
	return nil
}

// Logout ends the session. It is a no-op without one.
func (c *Client) Logout(ctx context.Context) error {
	if c.SID == "" {
		return nil
	}
	u := c.BaseURL.JoinPath("login_sid.lua")
	u.RawQuery = url.Values{"logout": {"1"}, "sid": {c.SID}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("logout: %w", err)
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, res.Body)
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("logout: unexpected status %s", res.Status)
	}
	c.SID = ""
	return nil
}

func (c *Client) sessionInfo(ctx context.Context, method, u string, body io.Reader) (*sessionInfo, error) {
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("login: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("login: unexpected status %s", res.Status)
	}
	var info sessionInfo
	if err := xml.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&info); err != nil {
		return nil, fmt.Errorf("login: decode session info: %w", err)
	}
	return &info, nil
}

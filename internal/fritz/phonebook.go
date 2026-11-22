package fritz

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"fritzbook/internal/fbxml"
)

// Phonebook identifies a phonebook on the box.
type Phonebook struct {
	ID   int
	Name string
}

var (
	// ErrPhonebookNotFound is returned when no phonebook has the requested name.
	ErrPhonebookNotFound = errors.New("phonebook not found")
	// ErrAmbiguousPhonebook is returned when several phonebooks have the requested name.
	ErrAmbiguousPhonebook = errors.New("ambiguous phonebook")
	// ErrUnexpectedPhonebook is returned when an export delivers a phonebook other than the requested one.
	ErrUnexpectedPhonebook = errors.New("unexpected phonebook in export")
	// ErrConfirmationTimeout is returned when the box waited for a second factor and the
	// context deadline passed first.
	ErrConfirmationTimeout = errors.New("confirmation on the FRITZ!Box was not given in time")
)

// ImportError reports that the box refused an import. Message contains no secrets.
type ImportError struct {
	Status  int
	Message string
}

func (e *ImportError) Error() string {
	return fmt.Sprintf("the FRITZ!Box rejected the import (status %d): %s", e.Status, e.Message)
}

// ConfirmFunc is called once when the box seems to wait for the second factor.
type ConfirmFunc func(hint string)

const defaultConfirmAfter = 10 * time.Second

const tr064Hint = `enable "Allow access for applications" (Home Network > Network > Network Settings) on the FRITZ!Box and give the user the required permissions`

// tr064Unavailable reports whether err looks like disabled TR-064 or missing permissions:
// 401/403/404 answers and transport errors. Faults and other statuses are not.
func tr064Unavailable(err error) bool {
	var se *statusError
	if errors.As(err, &se) {
		return se.Status == http.StatusUnauthorized || se.Status == http.StatusForbidden || se.Status == http.StatusNotFound
	}
	var ne net.Error
	return errors.Is(err, ErrBadCredentials) || errors.As(err, &ne)
}

// lookupError has exactly the brief's message and matches its sentinel with errors.Is.
type lookupError struct {
	msg      string
	sentinel error
}

func (e *lookupError) Error() string        { return e.msg }
func (e *lookupError) Is(target error) bool { return target == e.sentinel }

// ListPhonebooks lists the phonebooks via TR-064.
func (c *Client) ListPhonebooks(ctx context.Context) ([]Phonebook, error) {
	ids, err := c.PhonebookIDsTR064(ctx)
	if err != nil {
		if ctx.Err() == nil && tr064Unavailable(err) {
			return nil, fmt.Errorf("%w: TR-064 is not available; %s, and check the user's permissions", err, tr064Hint)
		}
		return nil, err
	}
	pbs := make([]Phonebook, 0, len(ids))
	for _, id := range ids {
		name, _, err := c.PhonebookTR064(ctx, id)
		if err != nil {
			return nil, err
		}
		pbs = append(pbs, Phonebook{ID: id, Name: name})
	}
	return pbs, nil
}

// FindPhonebook returns the phonebook whose name equals name exactly (case-sensitive).
func (c *Client) FindPhonebook(ctx context.Context, name string) (Phonebook, error) {
	pbs, err := c.ListPhonebooks(ctx)
	if err != nil {
		return Phonebook{}, err
	}
	var found []Phonebook
	for _, p := range pbs {
		if p.Name == name {
			found = append(found, p)
		}
	}
	switch len(found) {
	case 0:
		return Phonebook{}, &lookupError{fmt.Sprintf("phonebook %q not found — create it in the FRITZ!Box web UI first", name), ErrPhonebookNotFound}
	case 1:
		return found[0], nil
	}
	return Phonebook{}, &lookupError{fmt.Sprintf("several phonebooks are named %q", name), ErrAmbiguousPhonebook}
}

// ExportPhonebook returns the phonebook XML via the multipart firmwarecfg export. The box
// needs the phonebook's real name. The answer is validated as phonebook XML.
func (c *Client) ExportPhonebook(ctx context.Context, id int, name string) ([]byte, error) {
	status, body, err := c.exportMultipart(ctx, id, name, true)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("export of phonebook %d was refused (status %d): the session or the request was rejected", id, status)
	}
	info, err := fbxml.Inspect(strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("export of phonebook %d is not a phonebook: %w", id, err)
	}
	if info.Name != name { // the backup must be the book that will be replaced
		return nil, fmt.Errorf("%w: asked for %q, got %q", ErrUnexpectedPhonebook, name, info.Name)
	}
	return body, nil
}

// ImportPhonebook replaces phonebook id with xml. Status 200 means accepted (the localized
// answer is never interpreted); callers verify by exporting again. If the box has not
// answered after Client.ConfirmAfter, onConfirm (may be nil) is called once and the call
// keeps waiting until ctx ends, which is then reported as ErrConfirmationTimeout.
func (c *Client) ImportPhonebook(ctx context.Context, id int, xml []byte, onConfirm ConfirmFunc) error {
	type result struct {
		status int
		err    error
	}
	done := make(chan result, 1)
	go func() {
		status, _, err := c.importRaw(ctx, id, xml, true)
		done <- result{status, err}
	}()
	after := c.ConfirmAfter
	if after <= 0 {
		after = defaultConfirmAfter
	}
	timer := time.NewTimer(after)
	defer timer.Stop()
	confirming := false
	var r result
	select {
	case r = <-done:
	case <-timer.C:
		select {
		case r = <-done: // the answer arrived at the same moment; nothing to confirm
		default:
			confirming = true
			if onConfirm != nil {
				onConfirm("confirm the import on the FRITZ!Box (enter the code on a phone or press a button on the box)")
			}
			r = <-done
		}
	}
	if r.err != nil {
		if confirming && errors.Is(r.err, context.DeadlineExceeded) {
			return ErrConfirmationTimeout
		}
		return r.err
	}
	switch {
	case r.status == http.StatusOK:
		return nil
	case r.status >= 300 && r.status < 400:
		return &ImportError{Status: r.status, Message: "redirected to the login page; the session or the permissions were not sufficient"}
	default:
		return &ImportError{Status: r.status, Message: http.StatusText(r.status)}
	}
}

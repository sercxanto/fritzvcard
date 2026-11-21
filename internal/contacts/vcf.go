package contacts

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/emersion/go-vcard"
)

// ParseFiles parses the vCard files in argument order and concatenates the
// results without deduplication.
func ParseFiles(paths []string) ([]Contact, []Skipped, error) {
	var contacts []Contact
	var skipped []Skipped
	for _, p := range paths {
		cs, sk, err := parseFile(p)
		if err != nil {
			return nil, nil, err
		}
		contacts = append(contacts, cs...)
		skipped = append(skipped, sk...)
	}
	return contacts, skipped, nil
}

func parseFile(path string) ([]Contact, []Skipped, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	cs, sk, err := Parse(f)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", path, err)
	}
	return cs, sk, nil
}

// Parse reads all vCards from r.
func Parse(r io.Reader) ([]Contact, []Skipped, error) {
	var contacts []Contact
	var skipped []Skipped
	dec := vcard.NewDecoder(r)
	for {
		card, err := dec.Decode()
		if errors.Is(err, io.EOF) {
			return contacts, skipped, nil
		}
		if err != nil {
			return nil, nil, err
		}
		name := cardName(card)
		numbers := cardNumbers(card)
		switch {
		case name == "":
			skipped = append(skipped, Skipped{"(unnamed)", "no name"})
		case len(numbers) == 0:
			skipped = append(skipped, Skipped{name, "no phone number"})
		default:
			contacts = append(contacts, Contact{Name: name, Numbers: numbers, Emails: cardEmails(card)})
		}
	}
}

func cardName(card vcard.Card) string {
	if fn := strings.TrimSpace(card.PreferredValue(vcard.FieldFormattedName)); fn != "" {
		return fn
	}
	if n := card.Name(); n != nil {
		if s := strings.Join(strings.Fields(n.GivenName+" "+n.FamilyName), " "); s != "" {
			return s
		}
	}
	if f := card.Get(vcard.FieldOrganization); f != nil {
		org, _, _ := strings.Cut(f.Value, ";")
		return strings.TrimSpace(org)
	}
	return ""
}

// types returns the lower-cased, comma-split values of all TYPE params.
func types(f *vcard.Field) []string {
	var out []string
	for _, v := range f.Params[vcard.ParamType] {
		for _, t := range strings.Split(v, ",") {
			if t = strings.ToLower(strings.Trim(strings.TrimSpace(t), `"`)); t != "" {
				out = append(out, t)
			}
		}
	}
	return out
}

func contains(ts []string, want string) bool {
	for _, t := range ts {
		if t == want {
			return true
		}
	}
	return false
}

func cardNumbers(card vcard.Card) []Number {
	var out []Number
	prio := -1
	for _, f := range card[vcard.FieldTelephone] {
		value := strings.TrimSpace(f.Value)
		if len(value) >= 4 && strings.EqualFold(value[:4], "tel:") {
			value = strings.TrimSpace(value[4:])
		}
		ts := types(f)
		typ := "home"
		switch {
		case contains(ts, "fax"):
			typ = "fax_work"
		case contains(ts, "cell"):
			typ = "mobile"
		case contains(ts, "work"):
			typ = "work"
		}
		if prio < 0 && (contains(ts, "pref") || len(f.Params["PREF"]) > 0) {
			prio = len(out)
		}
		out = append(out, Number{Value: value, Type: typ})
	}
	if len(out) > 0 {
		if prio < 0 {
			prio = 0
		}
		out[prio].Prio = true
	}
	return out
}

func cardEmails(card vcard.Card) []Email {
	var out []Email
	for _, f := range card[vcard.FieldEmail] {
		c := "private"
		if contains(types(f), "work") {
			c = "work"
		}
		out = append(out, Email{Value: strings.TrimSpace(f.Value), Classifier: c})
	}
	return out
}

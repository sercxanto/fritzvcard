package fbxml

import (
	"encoding/xml"
	"errors"
	"io"
	"strings"
)

// ContactIdentity holds the contact fields checked after an upload.
type ContactIdentity struct {
	Name    string
	Numbers []string
}

// Info summarizes the first phonebook of a FRITZ!Box export.
type Info struct {
	Name     string
	Contacts int
	// Names holds the <realName> text of every direct-child <contact> (empty if a contact
	// has none), in document order; len(Names) == Contacts.
	Names   []string
	Entries []ContactIdentity
}

// Inspect reads the name and contact identities of the first <phonebook>.
func Inspect(r io.Reader) (Info, error) {
	dec := xml.NewDecoder(r)
	var info Info
	found := false
	depth := 0 // depth below the first phonebook element
	var cur strings.Builder
	inName, inContact := false, false
	inTelephony := false
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			if !found {
				return Info{}, errors.New("no <phonebook> element found")
			}
			return info, nil
		}
		if err != nil {
			return Info{}, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if !found {
				if t.Name.Local == "phonebook" {
					found = true
					for _, a := range t.Attr {
						if a.Name.Local == "name" {
							info.Name = a.Value
						}
					}
				}
				continue
			}
			depth++
			if depth == 1 {
				inContact = t.Name.Local == "contact"
			}
			if depth == 1 && inContact {
				info.Contacts++
				info.Names = append(info.Names, "")
				info.Entries = append(info.Entries, ContactIdentity{})
			}
			if depth == 2 {
				inTelephony = inContact && t.Name.Local == "telephony"
			}
			if depth == 3 && inTelephony && t.Name.Local == "number" {
				var number string
				if err := dec.DecodeElement(&number, &t); err != nil {
					return Info{}, err
				}
				entry := &info.Entries[len(info.Entries)-1]
				entry.Numbers = append(entry.Numbers, strings.TrimSpace(number))
				depth-- // DecodeElement consumed the end token.
			}
			if depth == 3 && t.Name.Local == "realName" && inContact {
				inName = true
				cur.Reset()
			}
		case xml.CharData:
			if inName {
				cur.Write(t)
			}
		case xml.EndElement:
			if found {
				if depth == 0 {
					return info, nil
				}
				if inName && depth == 3 {
					info.Names[len(info.Names)-1] = strings.TrimSpace(cur.String())
					info.Entries[len(info.Entries)-1].Name = info.Names[len(info.Names)-1]
					inName = false
				}
				depth--
			}
		}
	}
}

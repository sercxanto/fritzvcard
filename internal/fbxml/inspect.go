package fbxml

import (
	"encoding/xml"
	"errors"
	"io"
)

// Info summarizes the first phonebook of a FRITZ!Box export.
type Info struct {
	Name     string
	Contacts int
}

// Inspect reads the name and contact count of the first <phonebook>.
func Inspect(r io.Reader) (Info, error) {
	dec := xml.NewDecoder(r)
	var info Info
	found := false
	depth := 0 // depth below the first phonebook element
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
			if depth == 1 && t.Name.Local == "contact" {
				info.Contacts++
			}
		case xml.EndElement:
			if found {
				if depth == 0 {
					return info, nil
				}
				depth--
			}
		}
	}
}

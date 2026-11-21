// Package fbxml writes and inspects FRITZ!Box phonebook XML.
package fbxml

import (
	"encoding/xml"
	"io"

	"fritzbook/internal/contacts"
)

type phonebooks struct {
	XMLName   xml.Name  `xml:"phonebooks"`
	Phonebook phonebook `xml:"phonebook"`
}

type phonebook struct {
	Name     string    `xml:"name,attr"`
	Contacts []contact `xml:"contact"`
}

type contact struct {
	Category  int       `xml:"category"`
	Person    person    `xml:"person"`
	Telephony telephony `xml:"telephony"`
	Services  *services `xml:"services"`
	Setup     string    `xml:"setup"`
}

type person struct {
	RealName string `xml:"realName"`
}

type telephony struct {
	Numbers []number `xml:"number"`
}

type number struct {
	Type  string `xml:"type,attr"`
	Prio  int    `xml:"prio,attr"`
	ID    int    `xml:"id,attr"`
	Value string `xml:",chardata"`
}

type services struct {
	Emails []email `xml:"email"`
}

type email struct {
	Classifier string `xml:"classifier,attr"`
	ID         int    `xml:"id,attr"`
	Value      string `xml:",chardata"`
}

// Write encodes cs as a FRITZ!Box phonebook named phonebookName.
// Unique IDs are left out; the box assigns them on import.
func Write(w io.Writer, phonebookName string, cs []contacts.Contact) error {
	pb := phonebook{Name: phonebookName}
	for _, c := range cs {
		fc := contact{Person: person{RealName: c.Name}}
		for i, n := range c.Numbers {
			prio := 0
			if n.Prio {
				prio = 1
			}
			fc.Telephony.Numbers = append(fc.Telephony.Numbers, number{Type: n.Type, Prio: prio, ID: i, Value: n.Value})
		}
		if len(c.Emails) > 0 {
			fc.Services = &services{}
			for i, e := range c.Emails {
				fc.Services.Emails = append(fc.Services.Emails, email{Classifier: e.Classifier, ID: i, Value: e.Value})
			}
		}
		pb.Contacts = append(pb.Contacts, fc)
	}
	if _, err := io.WriteString(w, `<?xml version="1.0" encoding="utf-8"?>`+"\n"); err != nil {
		return err
	}
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(phonebooks{Phonebook: pb}); err != nil {
		return err
	}
	_, err := io.WriteString(w, "\n")
	return err
}

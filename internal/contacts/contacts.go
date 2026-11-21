// Package contacts converts vCard exports into a simple contact model.
package contacts

// Contact is a named person or organization with phone numbers and emails.
type Contact struct {
	Name    string
	Numbers []Number
	Emails  []Email
}

// Number is a phone number. Type is one of "home", "mobile", "work", "fax_work".
// Exactly one number per contact has Prio set.
type Number struct {
	Value string
	Type  string
	Prio  bool
}

// Email is an email address. Classifier is "private" or "work".
type Email struct {
	Value      string
	Classifier string
}

// Skipped describes a vCard that did not yield a contact.
// Reason is "no phone number" or "no name".
type Skipped struct {
	Name   string
	Reason string
}

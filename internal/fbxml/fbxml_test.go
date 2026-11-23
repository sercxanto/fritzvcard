package fbxml

import (
	"bytes"
	"os"
	"reflect"
	"strings"
	"testing"

	"fritzvcard/internal/contacts"
)

var erika = contacts.Contact{
	Name:    "Erika Mustermann",
	Numbers: []contacts.Number{{Value: "+49 170 0000001", Type: "mobile", Prio: true}, {Value: "030 0000002", Type: "home"}},
	Emails:  []contacts.Email{{Value: "erika@example.org", Classifier: "private"}},
}

func TestInspect_ContactNumbers(t *testing.T) {
	const src = `<phonebook><contact><person><realName>A &amp; B</realName><number>ignore</number></person>
<telephony><number> +12-34 </number><number>**6&#49;0</number></telephony></contact>
<contact><person><realName>C</realName></person><telephony><number>005</number></telephony></contact>
<other><telephony><number>ignore</number></telephony></other></phonebook>`
	info, err := Inspect(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	want := []ContactIdentity{{"A & B", []string{"+12-34", "**610"}}, {"C", []string{"005"}}}
	if !reflect.DeepEqual(info.Entries, want) {
		t.Errorf("entries = %#v, want %#v", info.Entries, want)
	}
}

func TestWrite_Golden(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, "Nextcloud", []contacts.Contact{erika}); err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("testdata/basic.golden.xml")
	if err != nil {
		t.Fatal(err)
	}
	if buf.String() != string(want) {
		t.Errorf("got:\n%s\nwant:\n%s", buf.String(), want)
	}
}

func TestWrite_EscapesSpecialChars(t *testing.T) {
	var buf bytes.Buffer
	c := contacts.Contact{Name: "Müller & Söhne <GmbH>", Numbers: []contacts.Number{{Value: "1", Type: "work", Prio: true}}}
	if err := Write(&buf, "Nextcloud", []contacts.Contact{c}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "Müller &amp; Söhne &lt;GmbH&gt;") {
		t.Errorf("not escaped:\n%s", buf.String())
	}
	if strings.Contains(buf.String(), "<services>") {
		t.Errorf("services should be omitted:\n%s", buf.String())
	}
	info, err := Inspect(&buf)
	if err != nil || info.Name != "Nextcloud" || info.Contacts != 1 || !reflect.DeepEqual(info.Names, []string{c.Name}) {
		t.Errorf("info=%+v err=%v", info, err)
	}
}

func TestInspect_BoxExport(t *testing.T) {
	const x = `<?xml version="1.0" encoding="utf-8"?>
<phonebooks><phonebook name="Test">
<contact><person><realName>A</realName></person></contact>
<contact><person><realName>B</realName></person></contact>
<contact><person><realName>C</realName></person></contact>
</phonebook></phonebooks>`
	info, err := Inspect(strings.NewReader(x))
	if err != nil || info.Name != "Test" || info.Contacts != 3 || !reflect.DeepEqual(info.Names, []string{"A", "B", "C"}) {
		t.Errorf("info=%+v err=%v", info, err)
	}
	if _, err := Inspect(strings.NewReader("<phonebooks><phonebook")); err == nil {
		t.Error("want error for malformed XML")
	}
}

func TestInspect_NamesOnlyFromTopLevelContacts(t *testing.T) {
	const x = `<phonebooks><phonebook name="T">
<contact><person><realName> Dup &amp; Co </realName></person><telephony><number>1</number></telephony></contact>
<contact><person><realName>Dup &amp; Co</realName></person></contact>
<contact><person></person></contact>
<other><x><realName>ignored</realName></x></other>
</phonebook></phonebooks>`
	info, err := Inspect(strings.NewReader(x))
	if err != nil {
		t.Fatal(err)
	}
	if info.Contacts != 3 || !reflect.DeepEqual(info.Names, []string{"Dup & Co", "Dup & Co", ""}) {
		t.Errorf("info=%+v", info)
	}
}

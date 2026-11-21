package contacts

import (
	"reflect"
	"strings"
	"testing"
)

func parseString(t *testing.T, s string) ([]Contact, []Skipped) {
	t.Helper()
	cs, sk, err := Parse(strings.NewReader(s))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return cs, sk
}

func card(lines ...string) string {
	return "BEGIN:VCARD\r\nVERSION:3.0\r\n" + strings.Join(lines, "\r\n") + "\r\nEND:VCARD\r\n"
}

func TestParse_Basic(t *testing.T) {
	cs, sk, err := ParseFiles([]string{"../../testdata/vcf/nextcloud-v3.vcf"})
	if err != nil || len(sk) != 0 {
		t.Fatalf("err=%v skipped=%v", err, sk)
	}
	want := Contact{
		Name:    "Erika Mustermann",
		Numbers: []Number{{"+49 170 0000001", "mobile", true}, {"030 0000002", "home", false}},
		Emails:  []Email{{"erika@example.org", "private"}},
	}
	if len(cs) != 2 || !reflect.DeepEqual(cs[0], want) {
		t.Fatalf("got %+v", cs)
	}
}

func TestParse_TypeMapping(t *testing.T) {
	tests := []struct {
		name, line, wantVal, wantType string
	}{
		{"work voice", "TEL;TYPE=WORK,VOICE:1", "1", "work"},
		{"quoted cell", `TEL;TYPE="cell,voice":2`, "2", "mobile"},
		{"repeated type fax", "TEL;TYPE=work;TYPE=fax:3", "3", "fax_work"},
		{"fax", "TEL;TYPE=fax:4", "4", "fax_work"},
		{"no type", "TEL:5", "5", "home"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cs, _ := parseString(t, card("FN:X", tt.line))
			if len(cs) != 1 || len(cs[0].Numbers) != 1 {
				t.Fatalf("got %+v", cs)
			}
			n := cs[0].Numbers[0]
			if n.Value != tt.wantVal || n.Type != tt.wantType || !n.Prio {
				t.Errorf("got %+v", n)
			}
		})
	}

	t.Run("v4 uri", func(t *testing.T) {
		cs, _, err := ParseFiles([]string{"../../testdata/vcf/v4.vcf"})
		if err != nil || len(cs) != 1 {
			t.Fatalf("err=%v cs=%+v", err, cs)
		}
		want := Number{"+49-170-0000003", "mobile", true}
		if cs[0].Numbers[0] != want {
			t.Errorf("got %+v", cs[0].Numbers[0])
		}
		if cs[0].Emails[0].Classifier != "work" {
			t.Errorf("email %+v", cs[0].Emails)
		}
	})

	t.Run("pref on second", func(t *testing.T) {
		cs, _ := parseString(t, card("FN:X", "TEL;TYPE=cell:1", "TEL;TYPE=home,pref:2"))
		ns := cs[0].Numbers
		if ns[0].Prio || !ns[1].Prio || ns[1].Type != "home" {
			t.Errorf("got %+v", ns)
		}
	})

	t.Run("PREF param", func(t *testing.T) {
		cs, _ := parseString(t, card("FN:X", "TEL:1", "TEL;PREF=1:2"))
		ns := cs[0].Numbers
		if ns[0].Prio || !ns[1].Prio {
			t.Errorf("got %+v", ns)
		}
	})
}

func TestParse_NameFallbacks(t *testing.T) {
	cs, sk := parseString(t,
		card("N:Muster;Max;;;", "TEL:1")+
			card("ORG:ACME GmbH;Sales", "TEL:2")+
			card("TEL:3"))
	if len(cs) != 2 || cs[0].Name != "Max Muster" || cs[1].Name != "ACME GmbH" {
		t.Fatalf("got %+v", cs)
	}
	if !reflect.DeepEqual(sk, []Skipped{{"(unnamed)", "no name"}}) {
		t.Fatalf("skipped %+v", sk)
	}
}

func TestParse_SkipsWithoutNumber(t *testing.T) {
	cs, sk := parseString(t, card("FN:Nur Mail", "EMAIL:a@example.org"))
	if len(cs) != 0 || !reflect.DeepEqual(sk, []Skipped{{"Nur Mail", "no phone number"}}) {
		t.Fatalf("cs=%+v sk=%+v", cs, sk)
	}
}

func TestParse_FoldedLineAndUmlauts(t *testing.T) {
	cs, _, err := ParseFiles([]string{"../../testdata/vcf/nextcloud-v3.vcf"})
	if err != nil || len(cs) != 2 {
		t.Fatalf("err=%v cs=%+v", err, cs)
	}
	if cs[1].Name != "Jürgen Müller" || cs[1].Numbers[0].Value != "+49 170 0000004" {
		t.Errorf("got %+v", cs[1])
	}
}

func TestParseFiles_ConcatenatesInOrder(t *testing.T) {
	cs, _, err := ParseFiles([]string{"../../testdata/vcf/v4.vcf", "../../testdata/vcf/nextcloud-v3.vcf"})
	if err != nil || len(cs) != 3 {
		t.Fatalf("err=%v cs=%+v", err, cs)
	}
	got := []string{cs[0].Name, cs[1].Name, cs[2].Name}
	want := []string{"Max Beispiel", "Erika Mustermann", "Jürgen Müller"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
}

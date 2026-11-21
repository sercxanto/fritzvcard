package cli

import (
	"flag"
	"io"
	"reflect"
	"testing"
)

func TestParseInterspersed_DoubleDash(t *testing.T) {
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	name := fs.String("name", "", "")
	got, err := parseInterspersed(fs, []string{"a", "--name", "Y", "--", "-x.vcf", "--name", "X", "--"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"a", "-x.vcf", "--name", "X", "--"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	if *name != "Y" {
		t.Fatalf("name = %q", *name)
	}
}

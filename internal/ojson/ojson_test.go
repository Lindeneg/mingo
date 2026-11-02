package ojson

import "testing"

func TestKeepsKeyOrderAndFormsLikeJq(t *testing.T) {
	in := `{"z": 1, "a": {"y": [1, 2.50, "x<y&z"], "b": {}}, "e": [], "n": null, "t": true, "u": "é"}`
	v, err := Parse([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	out, err := Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	want := `{
  "z": 1,
  "a": {
    "y": [
      1,
      2.50,
      "x<y&z"
    ],
    "b": {}
  },
  "e": [],
  "n": null,
  "t": true,
  "u": "é"
}
`
	if string(out) != want {
		t.Fatalf("got\n%s", out)
	}
}

func TestRefusesBrokenInput(t *testing.T) {
	for _, in := range []string{`{broken`, `{} {}`, ``} {
		if _, err := Parse([]byte(in)); err == nil {
			t.Fatal(in)
		}
	}
}

func TestSetAppendsANewKeyAndKeepsAnOldOnesPlace(t *testing.T) {
	o := NewObject()
	o.Set("a", "1")
	o.Set("b", "2")
	o.Set("a", "3")
	out, _ := Marshal(o)
	if string(out) != "{\n  \"a\": \"3\",\n  \"b\": \"2\"\n}\n" {
		t.Fatal(string(out))
	}
}

func TestMarshalIndentWritesWhatIndentFoundInTheInput(t *testing.T) {
	for _, in := range []string{"{\n    \"a\": [\n        1\n    ],\n    \"b\": {}\n}\n", "{\n\t\"a\": [\n\t\t1\n\t],\n\t\"b\": {}\n}\n"} {
		v, err := Parse([]byte(in))
		if err != nil {
			t.Fatal(err)
		}
		out, _ := MarshalIndent(v, Indent([]byte(in)))
		if string(out) != in {
			t.Fatal(string(out))
		}
	}
	for _, in := range []string{`{"a": 1}`, "", "{\n}\n"} {
		if got := Indent([]byte(in)); got != "  " {
			t.Fatalf("%q gave %q", in, got)
		}
	}
}

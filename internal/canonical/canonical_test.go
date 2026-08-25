package canonical

import (
	"bytes"
	"testing"
)

type goldenValue struct {
	Version string   `json:"version"`
	Name    string   `json:"name"`
	Count   int64    `json:"count"`
	Values  []string `json:"values"`
	Payload []byte   `json:"payload"`
}

func TestMarshalGoldenVector(t *testing.T) {
	value := goldenValue{
		Version: "fabric-json-v0",
		Name:    "<agent>",
		Count:   2,
		Values:  []string{"a", "b"},
		Payload: []byte("state"),
	}

	got, err := Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte(`{"version":"fabric-json-v0","name":"<agent>","count":2,"values":["a","b"],"payload":"c3RhdGU="}`)
	if !bytes.Equal(got, want) {
		t.Fatalf("canonical bytes mismatch\n got: %s\nwant: %s", got, want)
	}

	var decoded goldenValue
	if err := Decode(got, &decoded); err != nil {
		t.Fatal(err)
	}
	roundTrip, err := Marshal(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(roundTrip, want) {
		t.Fatalf("round-trip changed canonical bytes\n got: %s\nwant: %s", roundTrip, want)
	}
}

func TestMarshalRejectsMapsAndFloats(t *testing.T) {
	for _, value := range []any{
		struct {
			Value map[string]string `json:"value"`
		}{Value: map[string]string{"a": "b"}},
		struct {
			Value float64 `json:"value"`
		}{Value: 1.5},
	} {
		if _, err := Marshal(value); err == nil {
			t.Fatalf("Marshal(%T) unexpectedly succeeded", value)
		}
	}
}

package demo

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	var output bytes.Buffer
	result, err := Run(context.Background(), filepath.Join(t.TempDir(), "demo"), &output)
	if err != nil {
		t.Fatal(err)
	}
	if result.FinalRef == "" || result.DivergentRef == "" {
		t.Fatalf("demo returned incomplete result: %+v", result)
	}
	if !strings.Contains(output.String(), "PASS:") {
		t.Fatalf("demo output did not report success:\n%s", output.String())
	}
}

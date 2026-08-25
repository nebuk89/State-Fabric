package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestSplitCSVSortsAndDeduplicates(t *testing.T) {
	got := splitCSV("b, a,b")
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("unexpected CSV result: %v", got)
	}
}

func TestRunDemoCommand(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "demo")
	oldOutput := os.Stdout
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = write
	defer func() { os.Stdout = oldOutput }()

	done := make(chan []byte)
	go func() {
		var buffer bytes.Buffer
		_, _ = buffer.ReadFrom(read)
		done <- buffer.Bytes()
	}()
	if err := run([]string{"demo", "--dir", directory}); err != nil {
		t.Fatal(err)
	}
	write.Close()
	output := <-done
	if !bytes.Contains(output, []byte("PASS:")) {
		t.Fatalf("demo command did not pass:\n%s", output)
	}
}

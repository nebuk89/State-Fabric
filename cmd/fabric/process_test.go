//go:build !windows

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBetaFlowAcrossProcesses(t *testing.T) {
	root := t.TempDir()
	binary := filepath.Join(root, "fabric")
	build := exec.Command("go", "build", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build beta binary: %v\n%s", err, output)
	}
	authorityData := filepath.Join(root, "authority")
	actorData := filepath.Join(root, "actor")
	edgeData := filepath.Join(root, "edge")
	domain := filepath.Join(root, "domain.json")
	actorCapability := filepath.Join(root, "actor-capability.json")
	edgeCapability := filepath.Join(root, "edge-capability.json")

	runFabric(t, binary, "init", "--data", authorityData, "--authority")
	runFabric(t, binary, "domain-export", "--data", authorityData, "--out", domain)
	actorInit := runFabric(t, binary, "init", "--data", actorData, "--domain", domain)
	edgeInit := runFabric(t, binary, "init", "--data", edgeData, "--domain", domain)
	var actor, edge struct {
		PublicKey string `json:"public_key"`
	}
	if err := json.Unmarshal(actorInit, &actor); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(edgeInit, &edge); err != nil {
		t.Fatal(err)
	}
	runFabric(
		t,
		binary,
		"capability-issue",
		"--data", authorityData,
		"--subject", actor.PublicKey,
		"--namespace", "beta",
		"--operations", "transition.accept",
		"--out", actorCapability,
	)
	runFabric(
		t,
		binary,
		"capability-issue",
		"--data", authorityData,
		"--subject", edge.PublicKey,
		"--namespace", "beta",
		"--operations", "receipt.issue",
		"--out", edgeCapability,
	)
	source := strings.TrimSpace(string(runFabric(
		t, binary, "put", "--data", actorData, "--kind", "source", "--file", writeFixture(t, root, "source", "source"),
	)))
	workspaceRoot := strings.TrimSpace(string(runFabric(
		t, binary, "put", "--data", actorData, "--kind", "workspace", "--file", writeFixture(t, root, "workspace", "workspace"),
	)))
	provenance := strings.TrimSpace(string(runFabric(
		t, binary, "put", "--data", actorData, "--kind", "provenance", "--file", writeFixture(t, root, "provenance", "provenance"),
	)))
	transitionOutput := runFabric(
		t,
		binary,
		"transition",
		"--data", actorData,
		"--namespace", "beta",
		"--source", source,
		"--workspace", workspaceRoot,
		"--provenance", provenance,
		"--capability", actorCapability,
	)
	var transition struct {
		Transition struct {
			ID string `json:"id"`
		} `json:"transition"`
	}
	if err := json.Unmarshal(transitionOutput, &transition); err != nil {
		t.Fatal(err)
	}

	edgeAddress := freeAddress(t)
	edgeProcess := startFabricServer(t, binary, edgeData, edgeAddress)
	waitForHealth(t, "http://"+edgeAddress)
	receiptOutput := runFabric(
		t,
		binary,
		"accept",
		"--data", actorData,
		"--peer", "http://"+edgeAddress,
		"--transition", transition.Transition.ID,
		"--capability", edgeCapability,
	)
	var receipt struct {
		NodePublicKey string `json:"node_public_key"`
	}
	if err := json.Unmarshal(receiptOutput, &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.NodePublicKey != edge.PublicKey || receipt.NodePublicKey == actor.PublicKey {
		t.Fatal("receipt was not issued by the independent edge process")
	}
	stopFabricServer(t, edgeProcess)

	authorityAddress := freeAddress(t)
	authorityProcess := startFabricServer(t, binary, authorityData, authorityAddress)
	waitForHealth(t, "http://"+authorityAddress)
	runFabric(
		t,
		binary,
		"sync",
		"--data", edgeData,
		"--peer", "http://"+authorityAddress,
		"--transition", transition.Transition.ID,
	)
	finalized := runFabric(
		t,
		binary,
		"finalize",
		"--data", authorityData,
		"--transition", transition.Transition.ID,
	)
	if !bytes.Contains(finalized, []byte(`"status": "finalized"`)) {
		t.Fatalf("transition did not finalize:\n%s", finalized)
	}
	runFabric(t, binary, "verify", "--data", authorityData)
	stats := runFabric(
		t,
		binary,
		"stats",
		"--data", actorData,
		"--peer", "http://"+authorityAddress,
	)
	if !bytes.Contains(stats, []byte(`"transitions": 1`)) {
		t.Fatalf("remote stats did not report replicated transition:\n%s", stats)
	}
	stopFabricServer(t, authorityProcess)
}

func runFabric(t *testing.T, binary string, arguments ...string) []byte {
	t.Helper()
	command := exec.Command(binary, arguments...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("fabric %v: %v\n%s", arguments, err, output)
	}
	return output
}

func writeFixture(t *testing.T, root, name, content string) string {
	t.Helper()
	path := filepath.Join(root, name+".txt")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func freeAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return address
}

type serverProcess struct {
	command *exec.Cmd
	output  *bytes.Buffer
}

func startFabricServer(t *testing.T, binary, data, address string) serverProcess {
	t.Helper()
	var output bytes.Buffer
	command := exec.Command(binary, "serve", "--data", data, "--listen", address)
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	return serverProcess{command: command, output: &output}
}

func waitForHealth(t *testing.T, endpoint string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		response, err := http.Get(endpoint + "/v0/health")
		if err == nil {
			response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("server at %s did not become healthy", endpoint)
}

func stopFabricServer(t *testing.T, server serverProcess) {
	t.Helper()
	if err := server.command.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- server.command.Wait()
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("server exited with error: %v\n%s", err, server.output.String())
		}
	case <-time.After(10 * time.Second):
		_ = server.command.Process.Kill()
		t.Fatalf("server did not stop:\n%s", server.output.String())
	}
}

func Example_betaProcessFlow() {
	fmt.Println("fabric accept --peer https://edge.example --transition <txn> --capability edge-capability.json")
	// Output:
	// fabric accept --peer https://edge.example --transition <txn> --capability edge-capability.json
}

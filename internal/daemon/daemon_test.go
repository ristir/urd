package daemon

import (
	"bufio"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ristir/urd/internal/corpus"
	"github.com/ristir/urd/internal/histfile"
	"github.com/ristir/urd/internal/protocol"
	"github.com/ristir/urd/internal/query"
)

func testCache() *query.Cache {
	c, _ := corpus.Build([]histfile.Entry{
		{Cmd: "ansible-playbook rate-limit.yml", TS: 200, Source: "t"},
		{Cmd: "kubectl get pods", TS: 100, Source: "t"},
	})
	return query.NewCache(c, 32, query.DefaultDelims)
}

func TestServeAnswersRequest(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "urd.sock")
	ln, err := Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	go Serve(ln, testCache(), time.Minute)
	defer ln.Close()

	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	if _, err := conn.Write([]byte("Q 0 ans rate\n")); err != nil {
		t.Fatal(err)
	}
	res, err := protocol.DecodeResponse(bufio.NewReader(conn))
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 1 || res.Match == nil || res.Match.Cmd != "ansible-playbook rate-limit.yml" {
		t.Fatalf("got %+v", res)
	}
}

func TestServeKeepsConnectionOpen(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "urd.sock")
	ln, err := Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	go Serve(ln, testCache(), time.Minute)
	defer ln.Close()

	conn, _ := net.Dial("unix", sock)
	defer conn.Close()
	br := bufio.NewReader(conn)
	for i, q := range []string{"Q 0 ans\n", "Q 0 kube\n", "Q 0 ans rate\n"} {
		if _, err := conn.Write([]byte(q)); err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
		if _, err := protocol.DecodeResponse(br); err != nil {
			t.Fatalf("response %d: %v", i, err)
		}
	}
}

func TestListenArbitration(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "urd.sock")
	var mu sync.Mutex
	won := 0
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ln, err := Listen(sock)
			if err == nil {
				mu.Lock()
				won++
				mu.Unlock()
				time.Sleep(50 * time.Millisecond)
				ln.Close()
				return
			}
			if err != ErrAlreadyRunning {
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()
	if won != 1 {
		t.Fatalf("%d listeners won, want exactly 1", won)
	}
}

func TestListenRemovesStaleSocket(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "urd.sock")
	ln, err := Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	if unixLn, ok := ln.(*net.UnixListener); ok {
		unixLn.SetUnlinkOnClose(false)
	}
	ln.Close()

	ln2, err := Listen(sock)
	if err != nil {
		t.Fatalf("stale socket not reclaimed: %v", err)
	}
	ln2.Close()
}

func TestServeExitsOnIdle(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "urd.sock")
	ln, err := Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- Serve(ln, testCache(), 100*time.Millisecond) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("daemon did not exit on idle")
	}
}

func TestServeIgnoresGarbageRequest(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "urd.sock")
	ln, err := Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	go Serve(ln, testCache(), time.Minute)
	defer ln.Close()

	conn, _ := net.Dial("unix", sock)
	defer conn.Close()
	br := bufio.NewReader(conn)
	conn.Write([]byte("garbage\n"))
	res, err := protocol.DecodeResponse(br)
	if err != nil {
		t.Fatalf("garbage must get a well-formed empty answer: %v", err)
	}
	if res.Match != nil {
		t.Fatalf("got %+v, want empty result", res)
	}
	conn.Write([]byte("Q 0 ans\n"))
	if _, err := protocol.DecodeResponse(br); err != nil {
		t.Fatalf("connection unusable after garbage: %v", err)
	}
}

func TestServeExitsOnIdleWithStuckConnection(t *testing.T) {
	// t.TempDir() puts the test name in the path and overruns sockaddr_un (104 bytes on macOS).
	dir, err := os.MkdirTemp("", "urd")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	sock := filepath.Join(dir, "urd.sock")
	ln, err := Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	done := make(chan error, 1)
	go func() { done <- Serve(ln, testCache(), 100*time.Millisecond) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("daemon did not exit on idle while a connection sat open and silent")
	}
}

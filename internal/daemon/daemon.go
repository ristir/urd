package daemon

import (
	"bufio"
	"bytes"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/ristir/urd/internal/protocol"
	"github.com/ristir/urd/internal/query"
)

var ErrAlreadyRunning = errors.New("daemon: already running")

// Answerer is anything that can answer a query. The daemon knows no cache or corpus.
type Answerer interface {
	Search(q string, nav int) query.Result
}

// Arbitration rests on bind, not on lock files, which go stale after kill -9. A loop,
// because a failed bind can meet a socket another contender is mid-replacement of.
func Listen(sock string) (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(sock), 0o700); err != nil {
		return nil, err
	}
	var lastErr error
	for attempt := 0; attempt < 20; attempt++ {
		ln, err := net.Listen("unix", sock)
		if err == nil {
			os.Chmod(sock, 0o600)
			return ln, nil
		}
		lastErr = err
		if socketAlive(sock) {
			return nil, ErrAlreadyRunning
		}
		os.Remove(sock)
	}
	return nil, lastErr
}

// socketAlive retries the dial after a pause: bind() and listen() are separate
// syscalls, and an instant retry mistakes a descheduled winner for a dead socket.
func socketAlive(sock string) bool {
	for i := 0; i < 10; i++ {
		conn, err := net.DialTimeout("unix", sock, 200*time.Millisecond)
		if err == nil {
			conn.Close()
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

// Swappable allows replacing the corpus after a background rebuild without
// dropping open connections.
type Swappable struct {
	v atomic.Value
}

func NewSwappable(a Answerer) *Swappable {
	s := &Swappable{}
	s.v.Store(a)
	return s
}

func (s *Swappable) Set(a Answerer) { s.v.Store(a) }

func (s *Swappable) Search(q string, nav int) query.Result {
	return s.v.Load().(Answerer).Search(q, nav)
}

// Idle exit does not wait for connection goroutines: internal/protocol has no read
// timeouts, so a silent client parks one in ReadString and waiting never ends.
func Serve(ln net.Listener, a Answerer, idle time.Duration) error {
	var lastUse atomic.Int64
	lastUse.Store(time.Now().UnixNano())

	stop := make(chan struct{})
	go func() {
		tick := time.NewTicker(idle / 4)
		defer tick.Stop()
		for {
			select {
			case <-stop:
				return
			case <-tick.C:
				if time.Since(time.Unix(0, lastUse.Load())) > idle {
					ln.Close()
					return
				}
			}
		}
	}()
	defer close(stop)

	for {
		conn, err := ln.Accept()
		if err != nil {
			return nil
		}
		go serveConn(conn, a, &lastUse)
	}
}

func serveConn(conn net.Conn, a Answerer, lastUse *atomic.Int64) {
	defer conn.Close()
	br := bufio.NewReader(conn)
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return
		}
		lastUse.Store(time.Now().UnixNano())
		var res query.Result
		if req, perr := protocol.ParseRequest(line); perr == nil {
			res = a.Search(req.Query, req.Nav)
		}
		// The response is written in one call: the widget reads three lines after a
		// single zselect and must not have to collect them in pieces.
		var buf bytes.Buffer
		if eerr := protocol.EncodeResponse(&buf, res); eerr != nil {
			buf.Reset()
			protocol.EncodeResponse(&buf, query.Result{})
		}
		if _, werr := conn.Write(buf.Bytes()); werr != nil {
			return
		}
	}
}

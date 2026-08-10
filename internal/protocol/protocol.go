package protocol

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/ristir/urd/internal/query"
)

// Version comes first in the response: an updated binary with an old hook in an
// already open tab has to be noticed rather than half work.
const Version = "urd1"

var (
	ErrBadRequest  = errors.New("protocol: bad request")
	ErrBadResponse = errors.New("protocol: bad response")
	ErrNewline     = errors.New("protocol: command contains a newline")
)

type Request struct {
	Nav   int
	Query string
}

// ParseRequest reads the line "Q <nav> <query...>".
func ParseRequest(line string) (Request, error) {
	line = strings.TrimRight(line, "\r\n")
	if !strings.HasPrefix(line, "Q ") {
		return Request{}, ErrBadRequest
	}
	rest := line[2:]
	sp := strings.IndexByte(rest, ' ')
	navPart := rest
	q := ""
	if sp >= 0 {
		navPart = rest[:sp]
		q = rest[sp+1:]
	}
	nav, err := strconv.Atoi(navPart)
	if err != nil {
		return Request{}, ErrBadRequest
	}
	return Request{Nav: nav, Query: q}, nil
}

func EncodeResponse(w io.Writer, r query.Result) error {
	if r.Match == nil {
		_, err := fmt.Fprintf(w, "%s 0 0\n\n\n", Version)
		return err
	}
	if strings.ContainsAny(r.Match.Cmd, "\n\r") {
		return ErrNewline
	}
	spans := make([]string, 0, len(r.Match.Spans))
	for _, s := range r.Match.Spans {
		spans = append(spans, strconv.Itoa(s.Start)+":"+strconv.Itoa(s.End))
	}
	_, err := fmt.Fprintf(w, "%s %d %d\n%s\n%s\n", Version, r.Total, r.Index, r.Match.Cmd, strings.Join(spans, " "))
	return err
}

func DecodeResponse(br *bufio.Reader) (query.Result, error) {
	head, err := br.ReadString('\n')
	if err != nil {
		return query.Result{}, err
	}
	fields := strings.Fields(head)
	if len(fields) != 3 || fields[0] != Version {
		return query.Result{}, ErrBadResponse
	}
	total, err1 := strconv.Atoi(fields[1])
	idx, err2 := strconv.Atoi(fields[2])
	if err1 != nil || err2 != nil {
		return query.Result{}, ErrBadResponse
	}
	cmd, err := br.ReadString('\n')
	if err != nil {
		return query.Result{}, err
	}
	spanLine, err := br.ReadString('\n')
	if err != nil {
		return query.Result{}, err
	}
	if total == 0 {
		return query.Result{}, nil
	}
	res := query.Result{Total: total, Index: idx, Match: &query.Match{Cmd: strings.TrimRight(cmd, "\n")}}
	for _, tok := range strings.Fields(spanLine) {
		colon := strings.IndexByte(tok, ':')
		if colon < 0 {
			return query.Result{}, ErrBadResponse
		}
		a, e1 := strconv.Atoi(tok[:colon])
		b, e2 := strconv.Atoi(tok[colon+1:])
		if e1 != nil || e2 != nil {
			return query.Result{}, ErrBadResponse
		}
		res.Match.Spans = append(res.Match.Spans, query.Span{Start: a, End: b})
	}
	return res, nil
}

package privread

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"sync"
	"time"
)

const HelperFlag = "--internal-read-helper-v1"
const maxResponse = 4 << 20

type request struct {
	ID uint64    `json:"id"`
	Op Operation `json:"op"`
}

type response struct {
	ID   uint64          `json:"id"`
	Op   Operation       `json:"op"`
	Data json.RawMessage `json:"data"`
}

type greeting struct {
	Scope Scope     `json:"scope"`
	Until time.Time `json:"until"`
}

func readFrame(in *bufio.Reader, out any) error {
	line, err := in.ReadSlice('\n')
	if err != nil {
		if len(line) == 0 && errors.Is(err, io.EOF) {
			return err
		}
		return ErrProtocol
	}
	return decode(line, out)
}

func decode(data []byte, out any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil || d.Decode(new(json.RawMessage)) != io.EOF {
		return ErrProtocol
	}
	return nil
}

type client struct {
	pipe  io.ReadWriteCloser
	in    *bufio.Reader
	until time.Time
	scope Scope
	gate  chan struct{}
	once  sync.Once
	id    uint64
}

func connect(ctx context.Context, pipe io.ReadWriteCloser, scope Scope) (*client, error) {
	c := &client{pipe: pipe, in: bufio.NewReaderSize(pipe, maxResponse), scope: scope, gate: make(chan struct{}, 1)}
	stop := context.AfterFunc(ctx, c.close)
	defer stop()
	var hello greeting
	err := readFrame(c.in, &hello)
	if ctx.Err() != nil {
		err = ctx.Err()
	} else if err == io.EOF {
		err = ErrDenied
	} else if err == nil && (hello.Scope != scope || !hello.Until.After(time.Now()) || time.Until(hello.Until) > Lifetime) {
		err = ErrProtocol
	}
	if err != nil {
		c.close()
		return nil, err
	}
	c.until = hello.Until
	return c, nil
}

func (c *client) close() { c.once.Do(func() { _ = c.pipe.Close() }) }

func (c *client) read(ctx context.Context, op Operation, out any) error {
	ctx, cancel := context.WithDeadline(ctx, c.until)
	defer cancel()
	select {
	case c.gate <- struct{}{}:
		defer func() { <-c.gate }()
	case <-ctx.Done():
		return ctx.Err()
	}
	if !time.Now().Before(c.until) {
		return ErrExpired
	}
	stop := context.AfterFunc(ctx, c.close)
	defer stop()
	c.id++
	err := json.NewEncoder(c.pipe).Encode(request{ID: c.id, Op: op})
	var reply response
	if err == nil {
		err = readFrame(c.in, &reply)
	}
	switch {
	case !time.Now().Before(c.until):
		return ErrExpired
	case ctx.Err() != nil:
		return ctx.Err()
	case err != nil:
		if err == ErrProtocol {
			return err
		}
		return ErrDenied
	case reply.ID != c.id || reply.Op != op || len(reply.Data) == 0 || bytes.Equal(reply.Data, []byte("null")):
		return ErrProtocol
	default:
		return decode(reply.Data, out)
	}
}

func serve(ctx context.Context, pipe io.ReadWriter, scope Scope, operations []Operation, collect func(context.Context, Operation) any) error {
	ctx, cancel := context.WithTimeout(ctx, Lifetime)
	defer cancel()
	until, _ := ctx.Deadline()
	out := json.NewEncoder(pipe)
	if err := out.Encode(greeting{Scope: scope, Until: until}); err != nil {
		return err
	}
	requests := make(chan request)
	readErr := make(chan error, 1)
	go func() {
		in := bufio.NewReaderSize(pipe, 256)
		for expected := uint64(1); ; expected++ {
			var req request
			err := readFrame(in, &req)
			if err == nil && (req.ID != expected || !allowed(scope, req.Op) || !slices.Contains(operations, req.Op)) {
				err = ErrProtocol
			}
			if err != nil {
				readErr <- err
				cancel()
				return
			}
			select {
			case requests <- req:
			case <-ctx.Done():
				return
			}
		}
	}()
	for {
		select {
		case req := <-requests:
			data, err := json.Marshal(collect(ctx, req.Op))
			if err != nil || len(data) > maxResponse-128 {
				return ErrProtocol
			}
			if err := out.Encode(response{ID: req.ID, Op: req.Op, Data: data}); err != nil {
				return err
			}
		case <-ctx.Done():
			select {
			case err := <-readErr:
				return err
			default:
				return ctx.Err()
			}
		}
	}
}

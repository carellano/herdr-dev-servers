package herdr

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
)

const maxJSONLMessage = 64 << 10

var (
	ErrMalformedEnvelope = errors.New("malformed Herdr envelope")
	ErrMessageTooLarge   = errors.New("Herdr message exceeds limit")
)

type APIError struct{ Code, Message string }

func (e *APIError) Error() string { return fmt.Sprintf("Herdr %s: %s", e.Code, e.Message) }

// JSONLTransport is the low-level Herdr Unix-socket JSONL boundary. Higher-level
// operations remain capability-gated and must not infer live protocol behavior.
type JSONLTransport struct {
	Socket string
	Dial   func(context.Context, string) (net.Conn, error)
}

type request struct {
	ID     string `json:"id"`
	Method string `json:"method"`
	Params any    `json:"params"`
}
type response struct {
	ID     string                          `json:"id"`
	Result json.RawMessage                 `json:"result"`
	Error  *struct{ Code, Message string } `json:"error"`
	Raw    json.RawMessage                 `json:"-"`
}

func (t JSONLTransport) Call(ctx context.Context, id, method string, params, result any) error {
	conn, err := t.connect(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := json.NewEncoder(conn).Encode(request{ID: id, Method: method, Params: params}); err != nil {
		return fmt.Errorf("write Herdr request: %w", err)
	}
	reply, err := readResponse(bufio.NewReader(conn))
	if err != nil {
		return err
	}
	if reply.ID != id {
		return fmt.Errorf("Herdr response ID mismatch")
	}
	if reply.Error != nil {
		return &APIError{Code: reply.Error.Code, Message: reply.Error.Message}
	}
	if result != nil {
		return json.Unmarshal(reply.Result, result)
	}
	return nil
}

func (t JSONLTransport) connect(ctx context.Context) (net.Conn, error) {
	dial := t.Dial
	if dial == nil {
		dial = func(ctx context.Context, path string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", path)
		}
	}
	conn, err := dial(ctx, t.Socket)
	if err != nil {
		return nil, fmt.Errorf("connect to Herdr socket: %w", err)
	}
	return conn, nil
}

func readResponse(reader *bufio.Reader) (response, error) {
	var line []byte
	for {
		part, err := reader.ReadSlice('\n')
		if len(line)+len(part) > maxJSONLMessage {
			return response{}, ErrMessageTooLarge
		}
		line = append(line, part...)
		if err == nil {
			break
		}
		if !errors.Is(err, bufio.ErrBufferFull) {
			return response{}, fmt.Errorf("read Herdr response: %w", err)
		}
	}
	var reply response
	if err := json.Unmarshal(line, &reply); err != nil {
		return response{}, fmt.Errorf("%w: %v", ErrMalformedEnvelope, err)
	}
	reply.Raw = append(reply.Raw[:0], line...)
	return reply, nil
}

// Subscribe acknowledges correlation before returning a bounded asynchronous event stream.
func (t JSONLTransport) Subscribe(ctx context.Context, id string, subscriptions []Subscription) (<-chan Event, error) {
	conn, err := t.connect(ctx)
	if err != nil {
		return nil, err
	}
	if err := json.NewEncoder(conn).Encode(request{ID: id, Method: "events.subscribe", Params: SubscriptionRequest{Subscriptions: subscriptions}}); err != nil {
		conn.Close()
		return nil, err
	}
	reader := bufio.NewReader(conn)
	reply, err := readResponse(reader)
	if err != nil {
		conn.Close()
		return nil, err
	}
	if reply.ID != id {
		conn.Close()
		return nil, fmt.Errorf("Herdr response ID mismatch")
	}
	if reply.Error != nil {
		conn.Close()
		return nil, &APIError{Code: reply.Error.Code, Message: reply.Error.Message}
	}
	var ack struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(reply.Result, &ack) != nil || ack.Type != "subscription_started" {
		conn.Close()
		return nil, fmt.Errorf("%w: missing subscription_started", ErrMalformedEnvelope)
	}
	events := make(chan Event, 16)
	go func() {
		defer close(events)
		defer conn.Close()
		done := make(chan struct{})
		defer close(done)
		go func() {
			select {
			case <-ctx.Done():
				_ = conn.Close()
			case <-done:
			}
		}()
		for {
			reply, err := readResponse(reader)
			if err != nil {
				return
			}
			var envelope struct {
				Event json.RawMessage `json:"event"`
			}
			if json.Unmarshal(reply.Raw, &envelope) != nil || envelope.Event == nil {
				continue
			}
			var event Event
			if json.Unmarshal(envelope.Event, &event) != nil || event.Type == "" {
				continue
			}
			event.Raw = append(event.Raw[:0], envelope.Event...)
			select {
			case events <- event:
			default:
			}
		}
	}()
	return events, nil
}

func (t JSONLTransport) Snapshot(ctx context.Context, id string) (SnapshotResponse, error) {
	var result SnapshotResponse
	return result, t.Call(ctx, id, "session.snapshot", map[string]any{}, &result)
}
func (t JSONLTransport) ProcessInfo(ctx context.Context, id, paneID string) (ProcessInfoResponse, error) {
	var result ProcessInfoResponse
	return result, t.Call(ctx, id, "pane.process_info", ProcessInfoRequest{PaneID: paneID}, &result)
}
func (t JSONLTransport) ReportMetadata(ctx context.Context, id string, metadata MetadataRequest) error {
	return t.Call(ctx, id, "workspace.report_metadata", metadata, nil)
}

package herdr

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
)

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
}

func (t JSONLTransport) Call(ctx context.Context, id, method string, params, result any) error {
	dial := t.Dial
	if dial == nil {
		dial = func(ctx context.Context, path string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", path)
		}
	}
	conn, err := dial(ctx, t.Socket)
	if err != nil {
		return fmt.Errorf("connect to Herdr socket: %w", err)
	}
	defer conn.Close()
	if err := json.NewEncoder(conn).Encode(request{ID: id, Method: method, Params: params}); err != nil {
		return fmt.Errorf("write Herdr request: %w", err)
	}
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		return fmt.Errorf("read Herdr response: %w", err)
	}
	var reply response
	if err := json.Unmarshal(line, &reply); err != nil {
		return fmt.Errorf("decode Herdr response: %w", err)
	}
	if reply.ID != id {
		return fmt.Errorf("Herdr response ID mismatch")
	}
	if reply.Error != nil {
		return fmt.Errorf("Herdr %s: %s", reply.Error.Code, reply.Error.Message)
	}
	if result != nil {
		return json.Unmarshal(reply.Result, result)
	}
	return nil
}

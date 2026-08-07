package daemon

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"

	"github.com/carellano/herdr-apps/internal/model"
)

const IPCVersion = 1

// ServeJSONL handles one request at a time over a plugin-local JSONL stream.
func (s *Service) ServeJSONL(reader io.Reader, writer io.Writer) error {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		var request model.IPCRequest
		if err := json.Unmarshal(scanner.Bytes(), &request); err != nil {
			return writeResponse(writer, model.IPCResponse{Version: IPCVersion, Error: &model.IPCError{Code: "invalid_request", Message: err.Error()}})
		}
		response := model.IPCResponse{Version: IPCVersion, RequestID: request.RequestID}
		if request.Version != IPCVersion {
			response.Error = &model.IPCError{Code: "unsupported_version", Message: "IPC version is unsupported"}
		} else if request.Method != "list" {
			response.Error = &model.IPCError{Code: "unsupported_method", Message: "method is unavailable in the foundation release"}
		} else {
			snapshot := s.Snapshot()
			response.Revision = snapshot.Revision
			response.Result = snapshot
		}
		if err := writeResponse(writer, response); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func writeResponse(writer io.Writer, response model.IPCResponse) error {
	data, err := json.Marshal(response)
	if err != nil {
		return fmt.Errorf("encode IPC response: %w", err)
	}
	_, err = fmt.Fprintf(writer, "%s\n", data)
	return err
}

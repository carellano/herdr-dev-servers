package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/carellano/herdr-dev-servers/internal/model"
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
		} else if request.Method != "list" && request.Method != "inspect" && request.Method != "action" {
			response.Error = &model.IPCError{Code: "unsupported_method", Message: "method is unavailable in the foundation release"}
		} else {
			snapshot := s.Snapshot()
			response.Revision = snapshot.Revision
			if request.Method == "list" {
				response.Result = snapshot
			} else if request.Method == "inspect" {
				for _, application := range snapshot.Applications {
					if application.ID == request.Target {
						response.Result = application
						break
					}
				}
				if response.Result == nil {
					response.Error = &model.IPCError{Code: "not_found", Message: "application is unavailable"}
				}
			} else {
				result, revision, actionErr := s.ExecuteAction(context.Background(), request)
				response.Revision, response.Error, response.Result = revision, actionErr, result
			}
		}
		if err := writeResponse(writer, response); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func supportedAction(action string) bool {
	switch action {
	case "open", "copy", "focus", "terminate", "kill":
		return true
	default:
		return false
	}
}

func actionError(err error) *model.IPCError {
	var coded interface{ IPCCode() string }
	if errors.As(err, &coded) {
		return &model.IPCError{Code: coded.IPCCode(), Message: err.Error()}
	}
	return &model.IPCError{Code: "action_failed", Message: err.Error()}
}

func writeResponse(writer io.Writer, response model.IPCResponse) error {
	data, err := json.Marshal(response)
	if err != nil {
		return fmt.Errorf("encode IPC response: %w", err)
	}
	_, err = fmt.Fprintf(writer, "%s\n", data)
	return err
}

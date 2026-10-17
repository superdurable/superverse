package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/superdurable/dex-template-basic-process/internal/api/generated"
	"github.com/superdurable/dex-template-basic-process/internal/process"
	"github.com/superdurable/dex/sdk-go/dex"
)

// Handler implements openapi/openapi.yaml. Business state lives in Dex Flows;
// handlers start Flows and read their snapshots through the Dex Client.
type Handler struct {
	client *dex.Client
}

func NewHandler(client *dex.Client) (*generated.Server, error) {
	return generated.NewServer(&Handler{client: client},
		generated.WithErrorHandler(writeGeneratedError),
		generated.WithNotFound(func(w http.ResponseWriter, _ *http.Request) {
			writeError(w, http.StatusNotFound, "not_found", "unknown API route")
		}),
		generated.WithMethodNotAllowed(func(w http.ResponseWriter, _ *http.Request, allowed string) {
			w.Header().Set("Allow", allowed)
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method is not allowed")
		}),
	)
}

// NewError answers a handler error with a 500 and logs the end of the error;
// the response never carries it.
func (*Handler) NewError(ctx context.Context, err error) *generated.ErrorStatusCode {
	slog.ErrorContext(ctx, "API handler failed", "error", process.ErrorTail(err))
	return &generated.ErrorStatusCode{
		StatusCode: http.StatusInternalServerError,
		Response:   generated.ErrorResponse{Error: "application_unavailable", Message: "application is unavailable"},
	}
}

func (*Handler) GetHealth(context.Context) (*generated.HealthResponse, error) {
	return &generated.HealthResponse{Status: generated.HealthResponseStatusOk}, nil
}

func (handler *Handler) GetThreadApproval(ctx context.Context, params generated.GetThreadApprovalParams) (*generated.ThreadApproval, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	snapshot, found, err := process.ReadThreadApproval(ctx, handler.client, params.ApprovalId)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, apiError(http.StatusNotFound, "approval_not_found", "No thread approval exists with this ID.")
	}
	return &generated.ThreadApproval{
		Status:          generated.ThreadApprovalStatus(snapshot.Status),
		TeamId:          snapshot.Thread.TeamID,
		ChannelId:       snapshot.Thread.ChannelID,
		ThreadTimestamp: snapshot.Thread.ThreadTimestamp,
		ReplyUserId:     snapshot.ReplyUserID,
		Notice:          snapshot.Notice,
	}, nil
}

func apiError(status int, code, message string) *generated.ErrorStatusCode {
	return &generated.ErrorStatusCode{StatusCode: status, Response: generated.ErrorResponse{Error: code, Message: message}}
}

func writeGeneratedError(_ context.Context, w http.ResponseWriter, _ *http.Request, _ error) {
	writeError(w, http.StatusBadRequest, "invalid_request", "request does not match the OpenAPI contract")
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(generated.ErrorResponse{Error: code, Message: message})
}

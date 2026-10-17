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

type Handler struct{ client *dex.Client }

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

func (handler *Handler) Subscribe(ctx context.Context, request *generated.SubscribeRequest) (*generated.SubscribeAccepted, error) {
	email, err := process.NormalizeSubscriberEmail(request.Email)
	if err != nil {
		return nil, apiError(http.StatusBadRequest, "invalid_email", "Enter a valid email address.")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	_, err = process.Subscribe(ctx, handler.client, process.SubscriptionRequest{RequestID: request.RequestId.String(), Email: email})
	if err != nil {
		return nil, err
	}
	return &generated.SubscribeAccepted{Status: generated.SubscribeAcceptedStatusAccepted}, nil
}

func (handler *Handler) GetNewsletter(ctx context.Context, params generated.GetNewsletterParams) (*generated.Newsletter, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	snapshot, found, err := process.ReadNewsletter(ctx, handler.client, params.CampaignId)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, apiError(http.StatusNotFound, "newsletter_not_found", "No newsletter exists with this ID.")
	}
	return &generated.Newsletter{
		Status:  generated.NewsletterStatus(snapshot.Status),
		Subject: snapshot.Subject, Report: snapshot.Report,
		ReleaseCount: snapshot.ReleaseCount, RecipientCount: snapshot.RecipientCount,
		SentCount: snapshot.SentCount, FailedCount: snapshot.FailedCount,
		UnknownCount: snapshot.UnknownCount, Notice: snapshot.Notice,
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

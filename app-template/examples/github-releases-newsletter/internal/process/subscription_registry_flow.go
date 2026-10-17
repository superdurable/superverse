package process

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/mail"
	"sort"
	"strings"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
)

const SubscriberRegistryFlowID = "newsletter-subscribers"

type SubscriptionRequest struct {
	RequestID string `json:"requestId"`
	Email     string `json:"email"`
}

type Subscriber struct {
	Email        string    `json:"email"`
	SubscribedAt time.Time `json:"subscribedAt"`
}

type SubscriptionAccepted struct {
	Status string `json:"status"`
}

var (
	RegistryStatus       = dex.DefineAttribute[string]("registry-status")
	SubscriberCount      = dex.DefineAttribute[int64]("subscriber-count")
	Subscribers          = dex.DefineAttributeMap[Subscriber]("subscribers")
	SubscriptionRequests = dex.DefineChannel[SubscriptionRequest]("subscription-requests")
)

func NormalizeSubscriberEmail(raw string) (string, error) {
	email := strings.ToLower(strings.TrimSpace(raw))
	if email == "" || len(email) > 254 || strings.ContainsAny(email, " \t\r\n") {
		return "", fmt.Errorf("enter a valid email address")
	}
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email || !strings.Contains(email, "@") {
		return "", fmt.Errorf("enter a valid email address")
	}
	return email, nil
}

func subscriberKey(email string) string {
	digest := sha256.Sum256([]byte(email))
	return hex.EncodeToString(digest[:])
}

type SubscriptionRegistryFlow struct{ dex.FlowDefaults }

func (SubscriptionRegistryFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{dex.DefineStartStep(AwaitSubscription{}), dex.DefineStep(RecordSubscription{})}
}

func (flow SubscriptionRegistryFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
		dex.DefineRPC(flow.SubmitSubscription, nil),
		dex.DefineRPC(flow.ListSubscribers, &dex.RPCOptions{LoadAttributeMaps: []dex.AttributeDef{Subscribers}}),
	}
}

func (SubscriptionRegistryFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{
		Attributes: []dex.AttributeDef{RegistryStatus, SubscriberCount, Subscribers},
		Channels:   []dex.ChannelDef{SubscriptionRequests},
	}
}

// dex:group group-id:subscriptions group-label:"Subscriptions"
// dex:explanation text:"Wait for a visitor to submit an email address."
type AwaitSubscription struct{ dex.StepDefaults }

func (AwaitSubscription) WaitFor(ctx dex.Context, _ dex.None) (*dex.Wait, error) {
	if err := RegistryStatus.Set(ctx, "open"); err != nil {
		return nil, err
	}
	return dex.Until(SubscriptionRequests.ForOne()), nil
}

func (AwaitSubscription) Execute(ctx dex.Context, _ dex.None) (_ *dex.StepDecision, err error) {
	defer logFailure(ctx, SubscriptionRegistryFlow{}, "AwaitSubscription", &err)
	requests, err := SubscriptionRequests.GetConditionResults(ctx)
	if err != nil {
		return nil, err
	}
	if len(requests) != 1 {
		return nil, fmt.Errorf("expected one subscription request, received %d", len(requests))
	}
	key := subscriberKey(requests[0].Email)
	return dex.GoTo(RecordSubscription{}, requests[0], dex.WithStepOptions(&dex.StepOptions{
		ExecuteLoadAttributeMapInstances: []dex.AttributeMapLoad{Subscribers.Load(key)},
	})), nil
}

// dex:group group-id:subscriptions group-label:"Subscriptions"
// dex:explanation text:"Save a new subscriber once and preserve an existing subscription."
type RecordSubscription struct {
	dex.StepDefaultsNoWaitFor[SubscriptionRequest]
}

func (RecordSubscription) Execute(ctx dex.Context, request SubscriptionRequest) (_ *dex.StepDecision, err error) {
	defer logFailure(ctx, SubscriptionRegistryFlow{}, "RecordSubscription", &err)
	key := subscriberKey(request.Email)
	_, err = Subscribers.Get(ctx, key)
	var missing *dex.AttributeNotFoundError
	if err != nil && !errors.As(err, &missing) {
		return nil, err
	}
	if errors.As(err, &missing) {
		if err = Subscribers.Set(ctx, key, Subscriber{Email: request.Email, SubscribedAt: ctx.FirstAttemptAt()}); err != nil {
			return nil, err
		}
		count, getErr := optional(ctx, SubscriberCount)
		if getErr != nil {
			return nil, getErr
		}
		if err = SubscriberCount.Set(ctx, count+1); err != nil {
			return nil, err
		}
	}
	return dex.GoTo(AwaitSubscription{}, nil), nil
}

func (SubscriptionRegistryFlow) SubmitSubscription(ctx dex.Context, request SubscriptionRequest) (_ *dex.RPCResult[SubscriptionAccepted], err error) {
	defer logFailure(ctx, SubscriptionRegistryFlow{}, "SubmitSubscription", &err)
	request.Email, err = NormalizeSubscriberEmail(request.Email)
	if err != nil {
		return nil, err
	}
	if request.RequestID == "" {
		return nil, fmt.Errorf("request ID is required")
	}
	if err = SubscriptionRequests.Publish(ctx, request); err != nil {
		return nil, err
	}
	return &dex.RPCResult[SubscriptionAccepted]{Output: SubscriptionAccepted{Status: "accepted"}}, nil
}

func (SubscriptionRegistryFlow) ListSubscribers(ctx dex.Context, _ dex.None) (_ *dex.RPCResult[[]Subscriber], err error) {
	defer logFailure(ctx, SubscriptionRegistryFlow{}, "ListSubscribers", &err)
	keys := Subscribers.AllInstanceKeys(ctx)
	sort.Strings(keys)
	out := make([]Subscriber, 0, len(keys))
	for _, key := range keys {
		subscriber, getErr := Subscribers.Get(ctx, key)
		if getErr != nil {
			return nil, getErr
		}
		out = append(out, subscriber)
	}
	return &dex.RPCResult[[]Subscriber]{Output: out}, nil
}

// dex:field attribute-key:subscriber-count value-type:int64 editable:false description:"Subscribers"
func (SubscriptionRegistryFlow) GetDexSummary(ctx dex.Context, _ dex.None) (_ *dex.RPCResult[map[string]any], err error) {
	defer logFailure(ctx, SubscriptionRegistryFlow{}, "GetDexSummary", &err)
	count, err := optional(ctx, SubscriberCount)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"subscriber-count": count}}, nil
}

// dex:field attribute-key:registry-status value-type:string editable:false ui-slot:status description:"Status"
// dex:field attribute-key:subscriber-count value-type:int64 editable:false description:"Subscribers"
func (SubscriptionRegistryFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (_ *dex.RPCResult[map[string]any], err error) {
	defer logFailure(ctx, SubscriptionRegistryFlow{}, "GetDexDisplay", &err)
	status, err := optional(ctx, RegistryStatus)
	if err != nil {
		return nil, err
	}
	count, err := optional(ctx, SubscriberCount)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"registry-status": status, "subscriber-count": count}}, nil
}

func Subscribe(ctx context.Context, client *dex.Client, request SubscriptionRequest) (SubscriptionAccepted, error) {
	requestID := SubscriberRegistryFlowID
	_, err := client.StartFlow(ctx, SubscriptionRegistryFlow{}, SubscriberRegistryFlowID, nil, dex.StartFlowOptions{
		IDReusePolicy:  dex.IDReuseDisallow,
		AlreadyStarted: &dex.AlreadyStartedOptions{IgnoreError: true},
		RequestID:      &requestID,
	})
	if err != nil {
		return SubscriptionAccepted{}, err
	}
	var result SubscriptionAccepted
	err = client.InvokeRPC(ctx, SubscriberRegistryFlowID, SubscriptionRegistryFlow{}.SubmitSubscription, request, &result)
	return result, err
}

func optional[T any](ctx dex.Context, attribute dex.Attribute[T]) (T, error) {
	value, err := attribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return value, nil
	}
	return value, err
}

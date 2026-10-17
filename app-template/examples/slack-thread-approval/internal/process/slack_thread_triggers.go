package process

import (
	"slices"
	"strings"

	"github.com/superdurable/dex-connectors-library/connectors/slack"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// NewStartTriggerFilter admits a top-level message of the configured channel
// that matches the start binding's matcher.
func NewStartTriggerFilter(configuration slack.ChannelThreadCreatedTriggerConfiguration) (sdkgo.TriggerFilter[slack.MessageEvent], error) {
	if err := configuration.Validate(); err != nil {
		return nil, err
	}
	return messageFilter(configuration.ChannelID, configuration.ThreadTriggerMatcher, false), nil
}

// NewReplyTriggerFilter admits a thread reply of the configured channel that
// matches the reply binding's matcher.
func NewReplyTriggerFilter(configuration slack.ThreadReplyCreatedTriggerConfiguration) (sdkgo.TriggerFilter[slack.MessageEvent], error) {
	if err := configuration.Validate(); err != nil {
		return nil, err
	}
	return messageFilter(configuration.ChannelID, configuration.ThreadReplyMatcher, true), nil
}

// ResolveThreadApprovalFlowID gives a root message and its replies one FlowID.
func ResolveThreadApprovalFlowID(event sdkgo.TriggerEvent[slack.MessageEvent]) string {
	message := event.Payload
	return SlackThreadApprovalFlowID(message.TeamID, message.ChannelID, message.ThreadTimestamp)
}

// MapToThreadInput is the Start input of a root message.
func MapToThreadInput(event sdkgo.TriggerEvent[slack.MessageEvent]) ThreadInput {
	message := event.Payload
	return ThreadInput{EventID: event.ID, TeamID: message.TeamID, ChannelID: message.ChannelID, ThreadTimestamp: message.ThreadTimestamp}
}

// MapToThreadReply is the ReceiveThreadReply input of a reply: its event and
// author only, never the message text.
func MapToThreadReply(event sdkgo.TriggerEvent[slack.MessageEvent]) ThreadReply {
	return ThreadReply{EventID: event.ID, UserID: event.Payload.UserID}
}

func messageFilter(channelID string, matcher slack.MessageMatcher, reply bool) sdkgo.TriggerFilter[slack.MessageEvent] {
	return func(event sdkgo.TriggerEvent[slack.MessageEvent]) bool {
		message := event.Payload
		isReply := message.ThreadTimestamp != "" && message.ThreadTimestamp != message.Timestamp
		if event.ID == "" || message.TeamID == "" || message.ChannelID != channelID || message.ThreadTimestamp == "" || message.UserID == "" || isReply != reply {
			return false
		}
		if matcher.MessageContains != "" && !strings.Contains(strings.ToLower(message.Text), strings.ToLower(matcher.MessageContains)) {
			return false
		}
		return len(matcher.PosterUserIDs) == 0 || slices.Contains(matcher.PosterUserIDs, message.UserID)
	}
}

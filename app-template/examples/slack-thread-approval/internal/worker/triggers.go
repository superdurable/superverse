package worker

import (
	"fmt"
	"log/slog"

	"github.com/superdurable/dex-connectors-library/connectors/slack"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-template-basic-process/internal/connectorconfiguration"
	"github.com/superdurable/dex-template-basic-process/internal/process"
	"github.com/superdurable/dex/sdk-go/dex"
)

// OpenTriggers builds the Slack Trigger runner of the slack-approvals
// connection from the bindings saved in Studio Configuration: a new channel
// message starts SlackThreadApprovalFlow, a reply in its thread invokes
// ReceiveThreadReply. The runner serves the events the platform relays at
// slack.RelayedTriggerPath and runs until its context ends.
func OpenTriggers(configuration connectorconfiguration.Configuration, connections Connections, client *dex.Client, logger *slog.Logger) (*slack.MessageTriggerRunner, error) {
	project := configuration.Project()
	var start slack.ChannelThreadCreatedTriggerConfiguration
	if err := project.Configuration.DecodeTriggerConfiguration(slack.ConnectorID, process.SlackApprovalsConnectionName,
		slack.ChannelThreadCreatedTriggerDefinition.Trigger.TriggerName, process.StartTriggerBinding, &start); err != nil {
		return nil, fmt.Errorf("read the %s trigger binding: %w", process.StartTriggerBinding, err)
	}
	startFilter, err := process.NewStartTriggerFilter(start)
	if err != nil {
		return nil, fmt.Errorf("configure the %s trigger binding: %w", process.StartTriggerBinding, err)
	}
	var reply slack.ThreadReplyCreatedTriggerConfiguration
	if err := project.Configuration.DecodeTriggerConfiguration(slack.ConnectorID, process.SlackApprovalsConnectionName,
		slack.ThreadReplyCreatedTriggerDefinition.Trigger.TriggerName, process.ReplyTriggerBinding, &reply); err != nil {
		return nil, fmt.Errorf("read the %s trigger binding: %w", process.ReplyTriggerBinding, err)
	}
	replyFilter, err := process.NewReplyTriggerFilter(reply)
	if err != nil {
		return nil, fmt.Errorf("configure the %s trigger binding: %w", process.ReplyTriggerBinding, err)
	}
	flow := process.SlackThreadApprovalFlow{Connection: connections.SlackApprovals}
	runner, err := slack.NewProjectMessageTriggerRunner(project, process.SlackApprovalsConnectionName, slack.ProjectMessageTriggerRunnerConfig{
		ChannelThreadCreatedRoutes: []slack.ProjectChannelThreadCreatedTriggerRoute{
			{
				BindingName: process.StartTriggerBinding,
				Target:      sdkgo.NewDexFlowTriggerTarget(client, flow, startFilter, process.ResolveThreadApprovalFlowID, process.MapToThreadInput),
			},
		},
		ThreadReplyCreatedRoutes: []slack.ProjectThreadReplyCreatedTriggerRoute{
			{
				BindingName: process.ReplyTriggerBinding,
				Target:      sdkgo.NewDexRPCTriggerTarget(client, flow.ReceiveThreadReply, replyFilter, process.ResolveThreadApprovalFlowID, process.MapToThreadReply),
			},
		},
	}, slack.WithLogger(logger))
	if err != nil {
		return nil, fmt.Errorf("create the %s trigger runner: %w", process.SlackApprovalsConnectionName, err)
	}
	return runner, nil
}

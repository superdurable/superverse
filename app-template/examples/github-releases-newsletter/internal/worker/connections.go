package worker

import (
	"fmt"

	"github.com/superdurable/dex-connectors-library/connectors/github"
	"github.com/superdurable/dex-connectors-library/connectors/google/gmail"
	"github.com/superdurable/dex-connectors-library/connectors/slack"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/llm"
	"github.com/superdurable/dex-template-basic-process/internal/connectorconfiguration"
	"github.com/superdurable/dex-template-basic-process/internal/process"
)

type Connections struct {
	GitHub github.Connection
	Slack  slack.Connection
	OpenAI llm.Connection
	Gmail  gmail.Connection
}

func OpenConnections(configuration connectorconfiguration.Configuration) (Connections, error) {
	githubConnection, err := github.NewProjectConnection(configuration.Project(), process.GitHubConnectionName)
	if err != nil {
		return Connections{}, fmt.Errorf("configure GitHub connection: %w", err)
	}
	slackConnection, err := slack.NewProjectConnection(configuration.Project(), process.SlackConnectionName)
	if err != nil {
		return Connections{}, fmt.Errorf("configure Slack connection: %w", err)
	}
	openAIConnection, err := llm.NewProjectConnection(configuration.Project(), process.LLMConnectionName)
	if err != nil {
		return Connections{}, fmt.Errorf("configure OpenAI connection: %w", err)
	}
	gmailConnection, err := gmail.NewProjectConnection(configuration.Project(), process.GmailConnectionName)
	if err != nil {
		return Connections{}, fmt.Errorf("configure Gmail connection: %w", err)
	}
	return Connections{GitHub: githubConnection, Slack: slackConnection, OpenAI: openAIConnection, Gmail: gmailConnection}, nil
}

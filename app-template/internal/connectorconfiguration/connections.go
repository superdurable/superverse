// Package connectorconfiguration loads the project configuration that the user
// edits in Studio Configuration.
package connectorconfiguration

import (
	"context"
	"fmt"

	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

// Configuration is the project configuration that Superverse supplies to Preview
// and Live through the DEX_PROJECT_* environment.
type Configuration struct {
	project *projectconfig.LoadedProject
}

// Load verifies the project configuration and applies its application
// environment before the application creates clients, Workers or goroutines.
// The caller owns the deadline; an error prevents startup.
func Load(ctx context.Context) (Configuration, error) {
	project, err := projectconfig.LoadFromEnvironment(ctx)
	if err != nil {
		return Configuration{}, fmt.Errorf("load project configuration: %w", err)
	}
	environment, err := project.ResolveApplicationEnvironment(ctx)
	if err != nil {
		return Configuration{}, fmt.Errorf("resolve application environment: %w", err)
	}
	if err := environment.Apply(); err != nil {
		return Configuration{}, fmt.Errorf("apply application environment: %w", err)
	}
	return Configuration{project: project}, nil
}

// Project is the loaded project. Open each connection that dex-app.yaml declares
// with its connector's generated NewProjectConnection, such as
// llm.NewProjectConnection(configuration.Project(), name).
func (configuration Configuration) Project() *projectconfig.LoadedProject {
	return configuration.project
}

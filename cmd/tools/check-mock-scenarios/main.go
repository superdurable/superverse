// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// Command check-mock-scenarios loads the mock model's embedded scenarios and
// checks their coverage. Loading validates each against the Session tool
// catalog, its subagents' scripts against their kinds' tool sets, and both
// against the application template, and a broken scenario panics, so a
// template or tool change that breaks a script fails `make check` instead of a
// Session. Coverage is checked both ways: every tool of the Session catalog,
// every kind of pending user input, every notification kind that has a
// producer and every subagent kind appears in at least one scenario, and every
// scenario is launched by at least one Dex integration test
// (internal/**/*_integration_test.go, through mockInput) or E2E spec
// (web/studio/tests/e2e/**, through runMockScenario, sendMockInput or
// chooseMockFirstInput). It runs
// from the repository root.
package main

import (
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"strings"

	"github.com/superdurable/superversev2/internal/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/agents/session/agenttasks"
	"github.com/superdurable/superversev2/internal/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/agents/session/modelprovider/mockmodel"
)

// producedNotifications are the notification kinds a producer sends today:
// task by background commands and subagents, delivery by the Preview and the
// Live release and deployment Flows, and connector by the connector
// contribution Flows.
var producedNotifications = []conversation.NotificationKind{conversation.NotificationKindTask, conversation.NotificationKindDelivery, conversation.NotificationKindConnector}

// testRoots are where the tests that run scenarios live, with the file names
// that count as such tests.
var testRoots = []struct {
	directory string
	isTest    func(name string) bool
}{
	{"internal", func(name string) bool { return strings.HasSuffix(name, "_integration_test.go") }},
	{"web/studio/tests/e2e", func(name string) bool { return strings.HasSuffix(name, ".ts") }},
}

func main() {
	catalog := mockmodel.Catalog()
	problems := coverageProblems(catalog)
	tests, err := readTests()
	if err != nil {
		fmt.Fprintln(os.Stderr, "check-mock-scenarios:", err)
		os.Exit(1)
	}
	launched := launches(tests)
	remoteTests, err := readRemoteHTTPTests()
	if err != nil {
		fmt.Fprintln(os.Stderr, "check-mock-scenarios:", err)
		os.Exit(1)
	}
	problems = append(problems, remoteOnlyCoverageProblems(remoteTests)...)
	for _, scenario := range catalog {
		if !launched[scenario.Name] {
			problems = append(problems, "no Dex integration test or E2E spec launches the scenario "+scenario.Name+" through its helper")
		}
	}
	if len(problems) > 0 {
		fmt.Fprintln(os.Stderr, "Mock scenario coverage is incomplete; every new or changed tool, interaction and subagent kind ships with a scenario and a test that runs it (AGENTS.md, Test policy):")
		for _, problem := range problems {
			fmt.Fprintln(os.Stderr, "  -", problem)
		}
		os.Exit(1)
	}
	fmt.Printf("Mock scenarios: %d match the Session tool catalog and the application template, cover every tool, pending input, notification and subagent kind, and each runs in a test\n", len(catalog))
}

// Remote-only tools cannot be called by the platform mock model. Their
// coverage is a real remote HTTP Dex test that reports the call and syncs its
// result; platform tools still require the mock scenario above.
func remoteOnlyCoverageProblems(tests []string) []string {
	var problems []string
	for _, tool := range agentloop.RemoteTools() {
		if !tool.RemoteOnly {
			continue
		}
		constant := "conversation.Tool"
		for _, word := range strings.Split(string(tool.Name), "_") {
			constant += strings.ToUpper(word[:1]) + word[1:]
		}
		covered := false
		for _, source := range tests {
			if strings.Contains(source, constant) && strings.Contains(source, "bridgeapi.PathReport") && strings.Contains(source, "bridgeapi.PathSync") {
				covered = true
				break
			}
		}
		if !covered {
			problems = append(problems, fmt.Sprintf("no remote HTTP Dex integration test reports and syncs the remote-only tool %s", tool.Name))
		}
	}
	return problems
}

func readRemoteHTTPTests() ([]string, error) {
	entries, err := os.ReadDir("internal/agents")
	if err != nil {
		return nil, err
	}
	var tests []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, "session_remote_") || !strings.HasSuffix(name, "_integration_test.go") {
			continue
		}
		contents, err := os.ReadFile("internal/agents/" + name)
		if err != nil {
			return nil, err
		}
		if strings.Contains(string(contents), "HTTPDexIntegration") {
			tests = append(tests, string(contents))
		}
	}
	return tests, nil
}

// coverageProblems lists each tool, pending-input kind, produced
// notification kind and subagent kind that no scenario exercises.
func coverageProblems(catalog []mockmodel.Scenario) []string {
	tools := map[conversation.ToolName]bool{}
	pending := map[agentloop.PendingUserInputKind]bool{}
	notifications := map[conversation.NotificationKind]bool{}
	subagents := map[agenttasks.SubagentKind]bool{}
	for _, scenario := range catalog {
		for _, kind := range scenario.SubagentKinds {
			subagents[kind] = true
		}
		for _, tool := range scenario.Tools {
			tools[tool] = true
		}
		for _, input := range scenario.Inputs {
			if kind, answers := input.Kind.Answers(); answers {
				pending[kind] = true
			}
		}
		for _, kind := range scenario.Notifications {
			notifications[kind] = true
		}
	}
	var problems []string
	for _, definition := range agentloop.SessionTools(conversation.ModelMock) {
		if !tools[definition.Name] {
			problems = append(problems, fmt.Sprintf("no scenario calls the tool %s", definition.Name))
		}
	}
	for _, kind := range agentloop.PendingUserInputKinds {
		if !pending[kind] {
			name := string(kind)
			if name == "" {
				name = "question (request_user_input)"
			}
			problems = append(problems, fmt.Sprintf("no scenario input answers a pending %s input", name))
		}
	}
	for _, kind := range producedNotifications {
		if !notifications[kind] {
			problems = append(problems, fmt.Sprintf("no scenario receives a %s notification", kind))
		}
	}
	for _, kind := range agenttasks.SubagentKinds {
		if !subagents[kind] {
			problems = append(problems, fmt.Sprintf("no scenario starts a %s subagent", kind))
		}
	}
	return problems
}

// readTests returns the text of every test file that may run a scenario,
// read within each root only.
func readTests() ([]string, error) {
	var tests []string
	for _, testRoot := range testRoots {
		root, err := os.OpenRoot(testRoot.directory)
		if err != nil {
			return nil, err
		}
		files := root.FS()
		err = fs.WalkDir(files, ".", func(path string, entry fs.DirEntry, err error) error {
			switch {
			case err != nil:
				return err
			case entry.IsDir() && entry.Name() == "node_modules":
				return fs.SkipDir
			case entry.IsDir() || !testRoot.isTest(entry.Name()):
				return nil
			}
			text, readErr := fs.ReadFile(files, path)
			tests = append(tests, string(text))
			return readErr
		})
		if closeErr := root.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			return nil, err
		}
	}
	return tests, nil
}

// launchers are the test helper calls that launch a scenario: the Dex tests'
// mockInput(t, "<name>", n) and the E2E specs' runMockScenario and
// sendMockInput(page, project, "<name>", ...), whose Project addresses its one
// Session, and chooseMockFirstInput("<name>", ...), which sends input 1 from the
// creation page. A name in a comment or any other string launches nothing.
var launchers = []*regexp.Regexp{
	regexp.MustCompile(`mockInput\(t, "([a-z0-9][a-z0-9-]*)", `),
	regexp.MustCompile(`(?:runMockScenario|sendMockInput)\(\s*page,\s*[^,()]+,\s*"([a-z0-9][a-z0-9-]*)"`),
	regexp.MustCompile(`chooseMockFirstInput\(\s*"([a-z0-9][a-z0-9-]*)"`),
}

// launches is the set of scenarios the tests launch through their helpers.
func launches(tests []string) map[string]bool {
	launched := map[string]bool{}
	for _, test := range tests {
		for _, launcher := range launchers {
			for _, match := range launcher.FindAllStringSubmatch(test, -1) {
				launched[match[1]] = true
			}
		}
	}
	return launched
}

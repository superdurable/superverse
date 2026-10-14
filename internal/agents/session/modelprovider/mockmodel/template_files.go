// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package mockmodel

import (
	"encoding/json"
	"fmt"
	"path"
	"strings"

	"github.com/superdurable/superversev2/internal/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/agents/session/toolinput"
	"github.com/superdurable/superversev2/internal/app/apptemplate"
	"github.com/superdurable/superversev2/internal/comm"
)

// rootFiles are the files a script's file tools play on, by path: the
// application template's for a script whose root is the application, which
// lists every file, or only the files the script wrote for a connector_develop
// subagent, whose root is a worktree of the connectors library that the
// template does not describe.
type rootFiles struct {
	files    map[string]string
	complete bool
}

// playFileTool applies a file tool of a script to root, as the tool would in a
// Session that starts from the template: a read needs its file, an edit needs
// its old_string exactly once (at least once with replace_all) and a different
// new_string, and a write replaces the file. A template change that moves an
// edit's old text fails the scenario at startup instead of in a Session. An
// absolute path names a file outside the workspace, which the template does
// not describe, and is not checked, and neither is a file of the connectors
// library that the script did not write. A tool that mayFail, because its
// step expects a failure, changes nothing when it would fail.
func playFileTool(name conversation.ToolName, input json.RawMessage, root rootFiles, mayFail bool) error {
	files := root.files
	switch name {
	case conversation.ToolRead:
		arguments := comm.Must(toolinput.Read.Decode(string(input)))
		if file, relative := workspacePath(arguments.Path); relative {
			if _, found := files[file]; !found && root.complete && !mayFail {
				return fmt.Errorf("the application template has no file %q to read", file)
			}
		}
	case conversation.ToolEdit:
		arguments := comm.Must(toolinput.Edit.Decode(string(input)))
		file, relative := workspacePath(arguments.Path)
		if !relative {
			return fmt.Errorf("edit names %q; a scenario edits the workspace's files by relative path", arguments.Path)
		}
		contents, found := files[file]
		if !found && !root.complete {
			return nil
		}
		count := strings.Count(contents, arguments.OldString)
		var refusal error
		switch {
		case !found:
			refusal = fmt.Errorf("the application template has no file %q to edit", file)
		case arguments.OldString == arguments.NewString:
			refusal = fmt.Errorf("the edit of %q replaces old_string with itself", file)
		case count == 0:
			refusal = fmt.Errorf("old_string of the edit of %q is not in the application template", file)
		case count > 1 && !arguments.ReplaceAll.Value:
			refusal = fmt.Errorf("old_string of the edit of %q occurs %d times in the application template; make it unique or set replace_all", file, count)
		}
		if refusal != nil {
			if mayFail {
				return nil
			}
			return refusal
		}
		if arguments.ReplaceAll.Value {
			files[file] = strings.ReplaceAll(contents, arguments.OldString, arguments.NewString)
		} else {
			files[file] = strings.Replace(contents, arguments.OldString, arguments.NewString, 1)
		}
	case conversation.ToolWrite:
		arguments := comm.Must(toolinput.Write.Decode(string(input)))
		file, relative := workspacePath(arguments.Path)
		if !relative {
			return fmt.Errorf("write names %q; a scenario writes the workspace's files by relative path", arguments.Path)
		}
		files[file] = arguments.Content
	default:
	}
	return nil
}

// workspacePath is the clean relative path of a tool path, or false for an
// absolute one.
func workspacePath(name string) (string, bool) {
	if path.IsAbs(name) {
		return name, false
	}
	return path.Clean(name), true
}

// templateFiles are the contents of the application template's files by path.
func templateFiles() rootFiles {
	files := make(map[string]string)
	for _, file := range apptemplate.Current().Files {
		files[file.Path] = string(file.Contents)
	}
	return rootFiles{files: files, complete: true}
}

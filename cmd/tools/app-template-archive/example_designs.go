// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/superdurable/superversev2/internal/dexai/app/apptemplate"
)

// studioDesignDirectory holds Studio's generated designs, below the
// repository root: the default example's app-template.json, which the
// design-first start of a description compares with, every other example's
// examples/<id>.json, and example-designs.ts, which imports each on demand.
var studioDesignDirectory = filepath.Join("web", "studio", "lib", "generated", "template-design")

// defaultDesignFile is the default example's design in studioDesignDirectory.
const defaultDesignFile = "app-template.json"

// exampleDesign is an example's Flow definitions as its own FDG script renders
// them from its composed tree, under its template revision. A kept design is
// the committed one of a tree no changed path reaches, never written again.
type exampleDesign struct {
	id, tree, revision string
	definitions        json.RawMessage
	kept               bool
}

// designOf is an example's design: rendered when render holds the example,
// as it holds every example without changed paths, else the committed design
// when it names the example's composed tree, which then stays byte for byte,
// else rendered after all.
func designOf(ctx context.Context, root string, example composedExample, render map[string]bool) (exampleDesign, error) {
	if !render[example.manifest.ID] {
		committed, found, err := committedDesign(root, example)
		if err != nil || found {
			return committed, err
		}
	}
	fmt.Fprintf(os.Stderr, "app-template example %s: render its Flow definitions\n", example.manifest.ID)
	return renderExampleDesign(ctx, example)
}

// committedDesign reads the design that Studio's directory holds for an
// example and reports whether it names the example's composed tree.
func committedDesign(root string, example composedExample) (exampleDesign, bool, error) {
	template, err := apptemplate.Decode(mustPack(example.files))
	if err != nil {
		return exampleDesign{}, false, err
	}
	design := exampleDesign{id: example.manifest.ID, tree: template.TreeID, revision: apptemplate.ExampleRevision(example.manifest.ID, template.TreeID), kept: true}
	if design.id == apptemplate.DefaultExampleID {
		design.revision = template.Revision
	}
	contents, err := os.ReadFile(filepath.Join(root, studioDesignDirectory, filepath.FromSlash(design.file())))
	if errors.Is(err, os.ErrNotExist) {
		return exampleDesign{}, false, nil
	}
	if err != nil {
		return exampleDesign{}, false, err
	}
	var committed struct {
		TemplateRevision string          `json:"templateRevision"`
		Definitions      json.RawMessage `json:"definitions"`
	}
	if err = json.Unmarshal(contents, &committed); err != nil {
		return exampleDesign{}, false, fmt.Errorf("read the committed design of example %s: %w", design.id, err)
	}
	if committed.TemplateRevision != design.revision {
		return exampleDesign{}, false, nil
	}
	design.definitions = committed.Definitions
	return design, true, nil
}

// renderExampleDesign renders the Flow definitions of a composed example with
// its own FDG script, in a composed copy. The default example keeps the
// revision app-template@<tree> that a Project created from a description
// records; any other is app-template/<id>@<tree>.
func renderExampleDesign(ctx context.Context, example composedExample) (exampleDesign, error) {
	directory, err := os.MkdirTemp("", "app-template-design-")
	if err != nil {
		return exampleDesign{}, err
	}
	defer func() { _ = os.RemoveAll(directory) }()
	if err = materialize(example.files, directory); err != nil {
		return exampleDesign{}, err
	}
	template, err := apptemplate.Decode(mustPack(example.files))
	if err != nil {
		return exampleDesign{}, err
	}
	render := exec.CommandContext(ctx, "python3", "scripts/render-flow-definitions.py", "--output", "/dev/stdout")
	render.Dir, render.Stderr = directory, os.Stderr
	definitions, err := render.Output()
	if err != nil {
		return exampleDesign{}, fmt.Errorf("render the Flow definitions of example %s: %w", example.manifest.ID, err)
	}
	design := exampleDesign{id: example.manifest.ID, tree: template.TreeID, revision: template.Revision, definitions: definitions}
	if design.id != apptemplate.DefaultExampleID {
		design.revision = apptemplate.ExampleRevision(design.id, design.tree)
	}
	return design, nil
}

// steps are the Steps of the design's Flow definitions, by Flow type in the
// script's order and Step type in each graph's node order. A design without
// a Step is an error.
func (design exampleDesign) steps() ([]apptemplate.FlowStep, error) {
	var rendered []struct {
		FlowType string `json:"flowType"`
		Graph    struct {
			Nodes []struct {
				Kind string `json:"kind"`
				Name string `json:"name"`
			} `json:"nodes"`
		} `json:"graph"`
	}
	if err := json.Unmarshal(design.definitions, &rendered); err != nil {
		return nil, fmt.Errorf("read the Flow definitions of example %s: %w", design.id, err)
	}
	steps := []apptemplate.FlowStep{}
	for _, flow := range rendered {
		for _, node := range flow.Graph.Nodes {
			if node.Kind == "step" {
				steps = append(steps, apptemplate.FlowStep{FlowType: flow.FlowType, StepType: node.Name})
			}
		}
	}
	if len(steps) == 0 {
		return nil, fmt.Errorf("the Flow definitions of example %s declare no Step", design.id)
	}
	return steps, nil
}

// checkCardDesign requires the design on an example's card, which the
// Session records with an opened example's version 1 and the match rules
// count Steps from, to name exactly the (Flow type, Step type) pairs of the
// example's rendered Flow definitions, which the catalog's Steps and Studio
// draw: an edited Flow fails generation until its card says the same.
func checkCardDesign(example composedExample, rendered []apptemplate.FlowStep) error {
	var card struct {
		Flows []struct {
			FlowType string `json:"flow_type"`
			Steps    []struct {
				StepType string `json:"step_type"`
			} `json:"steps"`
		} `json:"flows"`
	}
	if err := json.Unmarshal(example.manifest.Design, &card); err != nil {
		return fmt.Errorf("example %s: read the card's design: %w", example.manifest.ID, err)
	}
	unmatched := map[apptemplate.FlowStep]int{}
	for _, step := range rendered {
		unmatched[step]++
	}
	for _, flow := range card.Flows {
		for _, step := range flow.Steps {
			unmatched[apptemplate.FlowStep{FlowType: flow.FlowType, StepType: step.StepType}]--
		}
	}
	var differences []string
	for step, count := range unmatched {
		switch {
		case count > 0:
			differences = append(differences, fmt.Sprintf("%s.%s is defined but not designed", step.FlowType, step.StepType))
		case count < 0:
			differences = append(differences, fmt.Sprintf("%s.%s is designed but not defined", step.FlowType, step.StepType))
		}
	}
	if len(differences) > 0 {
		slices.Sort(differences)
		return fmt.Errorf("example %s: the card's design differs from its Flow definitions: %s", example.manifest.ID, strings.Join(differences, "; "))
	}
	return nil
}

// file is the design's file in studioDesignDirectory, in slash form.
func (design exampleDesign) file() string {
	if design.id == apptemplate.DefaultExampleID {
		return defaultDesignFile
	}
	return path.Join("examples", design.id+".json")
}

// writeStudioDesigns writes every example's design to studioDesignDirectory,
// except a kept one, which is already there, and the module that names each
// example's tree and imports its design on demand, and removes the designs of
// examples that no longer exist.
func writeStudioDesigns(root string, designs []exampleDesign) error {
	directory := filepath.Join(root, studioDesignDirectory)
	written := map[string]bool{}
	var imports strings.Builder
	for _, design := range designs {
		target := filepath.Join(directory, filepath.FromSlash(design.file()))
		fmt.Fprintf(&imports, "  %q: { tree: %q, definitions: () => import(%q) },\n", design.id, design.tree, "./"+design.file())
		written[target] = true
		if design.kept {
			continue
		}
		var encoded bytes.Buffer
		encoder := json.NewEncoder(&encoded)
		encoder.SetEscapeHTML(false)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(struct {
			TemplateRevision string          `json:"templateRevision"`
			Definitions      json.RawMessage `json:"definitions"`
		}{design.revision, design.definitions}); err != nil {
			return err
		}
		if err := writeOutput(target, encoded.Bytes()); err != nil {
			return err
		}
	}
	if len(designs) == 0 {
		return errors.New("app-template has no example to write a design of")
	}
	module := filepath.Join(directory, "example-designs.ts")
	if err := writeOutput(module, studioDesignModule(imports.String())); err != nil {
		return err
	}
	written[module] = true
	return removeStale(directory, written)
}

// studioDesignModule is the TypeScript module of the examples' designs:
// each example's Git tree and the import of its rendered Flow definitions,
// which Studio's bundler splits into one chunk per example.
func studioDesignModule(imports string) []byte {
	return fmt.Appendf(nil, `// Code generated by cmd/tools/app-template-archive from app-template/; DO NOT EDIT.

/** An example's Flow definitions as its own FDG script renders them from its composed tree, under its template revision. */
export interface ExampleDesign {
  templateRevision: string;
  definitions: readonly {
    flowType: string;
    sourcePath: string;
    schemaVersion: string;
    digest: string;
    valid: boolean;
    graph: { valid?: unknown; schemaVersion?: unknown; flow?: { name?: unknown }; source?: { language?: unknown } };
  }[];
}

/** One example's Git tree and the import of its design, which only the Builder of a Project that starts from it runs. */
interface ExampleDesignImport {
  tree: string;
  definitions: () => Promise<{ default: ExampleDesign }>;
}

/** Every example's design, by example ID; the default example's is app-template.json. */
export const EXAMPLE_DESIGNS: Readonly<Record<string, ExampleDesignImport>> = {
%s};
`, imports)
}

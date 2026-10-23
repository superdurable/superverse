// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package goals

import (
	"fmt"
	"strings"
	"unicode"
)

// aggregate is a function of the metric language.
type aggregate string

const (
	aggregateCount aggregate = "count"
	aggregateRate  aggregate = "rate"
	aggregateSum   aggregate = "sum"
	aggregateMean  aggregate = "mean"
	aggregateP50   aggregate = "p50"
	aggregateP90   aggregate = "p90"
	aggregateMax   aggregate = "max"
)

// overOutcome reports whether the aggregate takes an outcome rather than a per-run number.
var overOutcome = map[aggregate]bool{
	aggregateCount: true, aggregateRate: true, aggregateSum: false, aggregateMean: false,
	aggregateP50: false, aggregateP90: false, aggregateMax: false,
}

// numberKind is a per-run number the export provides.
type numberKind string

const (
	numberDuration numberKind = "duration_h"
	numberSteps    numberKind = "steps"
	numberActions  numberKind = "actions"
	numberWait     numberKind = "wait_h"
	numberAI       numberKind = "ai"
	numberField    numberKind = "field"
)

// aiNumbers are the per-run AI usage numbers.
var aiNumbers = map[string]bool{"calls": true, "input_tokens": true, "output_tokens": true, "usd": true}

type number struct {
	kind numberKind
	name string
}

// key is the number's name in the metric language, such as steps.AwaitReply or duration_h.
func (n number) key() string {
	if n.kind == numberDuration {
		return string(numberDuration)
	}
	return string(n.kind) + "." + n.name
}

// condition restricts the runs of a term: an outcome, its negation, or a field's value.
type condition struct {
	negate  bool
	outcome string
	field   string
	value   string
}

type term struct {
	fn      aggregate
	outcome string
	of      string
	number  number
	where   *condition
}

// metric is a compiled metric: one term, or the ratio of two.
type metric struct {
	numerator   term
	denominator *term
}

// compileMetric parses a metric and checks every name it uses against the spec.
func compileMetric(source string, spec Spec) (metric, error) {
	p := &parser{tokens: tokenize(source)}
	numerator, err := p.term(spec)
	if err != nil {
		return metric{}, err
	}
	m := metric{numerator: numerator}
	if p.accept("/") {
		denominator, err := p.term(spec)
		if err != nil {
			return metric{}, err
		}
		m.denominator = &denominator
	}
	if !p.done() {
		return metric{}, fmt.Errorf("unexpected %q after the metric", p.peek())
	}
	return m, nil
}

type parser struct {
	tokens []string
	at     int
}

func (p *parser) peek() string {
	if p.done() {
		return ""
	}
	return p.tokens[p.at]
}

func (p *parser) done() bool { return p.at >= len(p.tokens) }

func (p *parser) next() string {
	token := p.peek()
	p.at++
	return token
}

func (p *parser) accept(token string) bool {
	if p.peek() == token {
		p.at++
		return true
	}
	return false
}

func (p *parser) expect(token string) error {
	if !p.accept(token) {
		return fmt.Errorf("expected %q, found %q", token, p.peek())
	}
	return nil
}

func (p *parser) term(spec Spec) (term, error) {
	name := aggregate(p.next())
	takesOutcome, known := overOutcome[name]
	if !known {
		return term{}, fmt.Errorf("unknown function %q; use count, rate, sum, mean, p50, p90 or max", name)
	}
	if err := p.expect("("); err != nil {
		return term{}, err
	}
	t := term{fn: name}
	if takesOutcome {
		t.outcome = p.next()
		if _, ok := spec.Outcomes[t.outcome]; !ok {
			return term{}, fmt.Errorf("unknown outcome %q", t.outcome)
		}
	} else {
		n, err := parseNumber(p.next(), spec)
		if err != nil {
			return term{}, err
		}
		t.number = n
	}
	for p.accept(",") {
		keyword := p.next()
		p.accept(":")
		switch keyword {
		case "of":
			if name != aggregateRate {
				return term{}, fmt.Errorf("only rate takes of <outcome>")
			}
			t.of = p.next()
			if _, ok := spec.Outcomes[t.of]; !ok {
				return term{}, fmt.Errorf("unknown outcome %q", t.of)
			}
		case "where":
			c, err := p.condition(spec)
			if err != nil {
				return term{}, err
			}
			t.where = &c
		default:
			return term{}, fmt.Errorf("unknown argument %q; use of <outcome> or where <condition>", keyword)
		}
	}
	if name == aggregateRate && t.of == "" {
		return term{}, fmt.Errorf("rate needs of <outcome>")
	}
	if err := p.expect(")"); err != nil {
		return term{}, err
	}
	return t, nil
}

func (p *parser) condition(spec Spec) (condition, error) {
	if p.accept("not") {
		outcome := p.next()
		if _, ok := spec.Outcomes[outcome]; !ok {
			return condition{}, fmt.Errorf("unknown outcome %q", outcome)
		}
		return condition{negate: true, outcome: outcome}, nil
	}
	name := p.next()
	if !p.accept("=") {
		if _, ok := spec.Outcomes[name]; !ok {
			return condition{}, fmt.Errorf("unknown outcome %q", name)
		}
		return condition{outcome: name}, nil
	}
	field := strings.TrimPrefix(name, "field.")
	if !contains(spec.Fields, field) {
		return condition{}, fmt.Errorf("field %q is not in fields", field)
	}
	value := p.next()
	if value == "" {
		return condition{}, fmt.Errorf("expected a value after %s =", name)
	}
	return condition{field: field, value: strings.Trim(value, `"`)}, nil
}

func parseNumber(token string, spec Spec) (number, error) {
	if token == string(numberDuration) {
		return number{kind: numberDuration}, nil
	}
	head, name, found := strings.Cut(token, ".")
	if !found || name == "" {
		return number{}, fmt.Errorf("unknown number %q; use duration_h, steps.<Step>, actions.<RPC>, wait_h.<Step>, ai.<usage> or field.<name>", token)
	}
	switch numberKind(head) {
	case numberSteps, numberActions, numberWait:
		return number{kind: numberKind(head), name: name}, nil
	case numberAI:
		if !aiNumbers[name] {
			return number{}, fmt.Errorf("unknown AI number %q; use calls, input_tokens, output_tokens or usd", name)
		}
		return number{kind: numberAI, name: name}, nil
	case numberField:
		if !contains(spec.Fields, name) {
			return number{}, fmt.Errorf("field %q is not in fields", name)
		}
		return number{kind: numberField, name: name}, nil
	}
	return number{}, fmt.Errorf("unknown number %q", token)
}

// tokenize splits a metric into names, quoted values and the punctuation ( ) , : / =.
func tokenize(source string) []string {
	var tokens []string
	runes := []rune(source)
	for i := 0; i < len(runes); {
		r := runes[i]
		switch {
		case unicode.IsSpace(r):
			i++
		case strings.ContainsRune("(),:/=", r):
			tokens = append(tokens, string(r))
			i++
		case r == '"':
			j := i + 1
			for j < len(runes) && runes[j] != '"' {
				j++
			}
			tokens = append(tokens, string(runes[i:min(j+1, len(runes))]))
			i = j + 1
		default:
			j := i
			for j < len(runes) && !unicode.IsSpace(runes[j]) && !strings.ContainsRune(`(),:/="`, runes[j]) {
				j++
			}
			tokens = append(tokens, string(runes[i:j]))
			i = j
		}
	}
	return tokens
}

func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package fileextract

import (
	"cmp"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/superdurable/superversev2/internal/comm"
)

// redactionMarker replaces every likely secret of a JSON file. The import
// inventory reports it as a literal secret to set up as a connection and to
// rotate, so it never passes for a real value.
const redactionMarker = "‹redacted:superverse›"

// maxRedactionPaths bounds the redactions a result lists; Redactions counts
// them all.
const maxRedactionPaths = 200

// JSONFormat is what a JSON file is: an n8n workflow export, or any other
// JSON. A struct that string literals cannot convert to; its values below
// are persisted by their names.
type JSONFormat struct{ name string }

var (
	JSONFormatN8N     = JSONFormat{"n8n"}
	JSONFormatGeneric = JSONFormat{"generic"}
)

func (f JSONFormat) String() string               { return f.name }
func (f JSONFormat) MarshalText() ([]byte, error) { return []byte(f.name), nil }
func (f *JSONFormat) UnmarshalText(text []byte) error {
	return comm.UnmarshalName(text, f, JSONFormat.String, JSONFormatN8N, JSONFormatGeneric)
}

// redactionRule is why a value was redacted: its field or header name, or
// its own shape inside a string. A struct that string literals cannot convert
// to; its values below are persisted by their names.
type redactionRule struct {
	name string
	// pattern finds the secret in a string; its last group, or else the
	// whole match, is the secret. Field and header rules have none.
	pattern *regexp.Regexp
}

var (
	ruleSecretField = redactionRule{name: "secret_field"}
	ruleSecretPair  = redactionRule{name: "secret_pair"}
	// RuleURLQuery is the inventory's URL_SECRET: a query parameter whose
	// name holds key, token, secret or password.
	ruleURLQuery      = redactionRule{"url_query", regexp.MustCompile(`(?i)[?&][A-Za-z_\-]*(?:key|token|secret|password)[A-Za-z_\-]*=([^&{}\s#'"]+)`)}
	ruleURLPassword   = redactionRule{"url_password", regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9+.\-]*://[^/\s:@"'<>]+:([^@/\s"'<>]+)@`)}
	ruleAuthorization = redactionRule{"authorization_credential", regexp.MustCompile(`(?i)\b(?:bearer|basic)\s+([a-z0-9._~+/\-]{16,}=*)`)}
	ruleAPIKey        = redactionRule{"api_key", regexp.MustCompile(`\b(?:sk-[A-Za-z0-9_\-]{16,}|(?:sk|rk)_(?:live|test)_[A-Za-z0-9]{16,}|AIza[0-9A-Za-z_\-]{35})`)}
	ruleSlackToken    = redactionRule{"slack_token", regexp.MustCompile(`\b(?:xox[abposr]-[A-Za-z0-9\-]{8,}|xapp-\d-[A-Za-z0-9\-]{8,})`)}
	ruleGitHubToken   = redactionRule{"github_token", regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,})`)}
	ruleAWSAccessKey  = redactionRule{"aws_access_key", regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`)}
	ruleJWT           = redactionRule{"jwt", regexp.MustCompile(`\beyJ[A-Za-z0-9_\-]{8,}\.eyJ[A-Za-z0-9_\-]{8,}\.[A-Za-z0-9_\-]{8,}`)}
	rulePrivateKey    = redactionRule{"private_key", regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?-----END [A-Z ]*PRIVATE KEY-----`)}
)

// stringRules are the rules that find a secret inside any string, in the
// order they apply.
var stringRules = []redactionRule{rulePrivateKey, ruleURLPassword, ruleURLQuery, ruleAuthorization, ruleAPIKey, ruleSlackToken, ruleGitHubToken, ruleAWSAccessKey, ruleJWT}

func (r redactionRule) String() string               { return r.name }
func (r redactionRule) MarshalText() ([]byte, error) { return []byte(r.name), nil }
func (r *redactionRule) UnmarshalText(text []byte) error {
	return comm.UnmarshalName(text, r, redactionRule.String, append([]redactionRule{ruleSecretField, ruleSecretPair}, stringRules...)...)
}

var (
	// secretName is a field or header name that holds a credential: the
	// inventory's SECRET_NAME, with access keys, passphrases and cookies.
	secretName = regexp.MustCompile(`(?i)(api.?key|access.?token|auth.?token|refresh.?token|token|secret|passw(or)?d|private.?key|authorization|bearer|access.?key|passphrase|cookie)`)
	// placeholder is a value that stands for a credential someone has yet to enter.
	placeholder = regexp.MustCompile(`(?i)^(your[_\-\s]|<.*>$|\[.*\]$|x{4,}|placeholder|changeme|change[_\-]me|insert[_\-\s]|replace[_\-\s]|enter[_\-\s])`)
	identifier  = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]*$`)
)

// n8nTriggers are the trigger node types that do not end with Trigger.
var n8nTriggers = []string{"cron", "interval", "start", "webhook"}

// PreparedJSON is a JSON file with its likely secrets redacted, what it is
// and its summary. It never holds a redacted value.
type PreparedJSON struct {
	Format JSONFormat `json:"format"`
	// Redacted is the redacted file, which the parent indents by two spaces.
	Redacted json.RawMessage `json:"redacted"`
	// Redactions counts the redacted values; RedactionPaths lists the first
	// 200 with their JSON paths, such as $.nodes[2].parameters.url.
	Redactions     int         `json:"redactions"`
	RedactionPaths []redaction `json:"redactionPaths"`
	Values         int         `json:"values"`
	Depth          int         `json:"depth"`
	// Workflows summarize an n8n export, one per workflow of the file.
	Workflows []Workflow `json:"workflows,omitempty"`
	// TopLevel is the type of the file's value, and Members the first 50
	// members of a generic object.
	TopLevel string       `json:"topLevel"`
	Members  []JSONMember `json:"members,omitempty"`
}

// JSONMember is one member of a generic object: its key, its value's type and
// an array's length.
type JSONMember struct {
	Key    string `json:"key"`
	Type   string `json:"type"`
	Length int    `json:"length,omitempty"`
}

// redaction is where one value was redacted and why.
type redaction struct {
	Path string        `json:"path"`
	Rule redactionRule `json:"rule"`
}

// Workflow summarizes one n8n workflow: its name, its nodes, the types they
// have, its triggers, its connections and the credentials it names.
type Workflow struct {
	Name        string          `json:"name"`
	Nodes       int             `json:"nodes"`
	Connections int             `json:"connections"`
	NodeTypes   []NodeTypeCount `json:"nodeTypes"`
	Triggers    []WorkflowNode  `json:"triggers"`
	Credentials []Credential    `json:"credentials"`
}

// NodeTypeCount is how many nodes of a workflow have a type.
type NodeTypeCount struct {
	Type  string `json:"type"`
	Count int    `json:"count"`
}

// WorkflowNode names one node.
type WorkflowNode struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// Credential is a credential a node names: its type and its name in n8n,
// never its value, which the export does not hold.
type Credential struct {
	Type string `json:"type"`
	Name string `json:"name"`
}

// prepareJSON parses, redacts and summarizes a JSON file.
func prepareJSON(data []byte) Result {
	root, values, depth, err := parseJSON(data)
	if errors.Is(err, errJSONBounds) {
		return refuse(FormatJSON, CodeTooLarge, err.Error())
	}
	if err != nil {
		return refuse(FormatJSON, CodeInvalidJSON, err.Error())
	}
	redactor := redactor{}
	redactor.value(root, "$")
	prepared := &PreparedJSON{
		Format: JSONFormatGeneric, Redacted: root.appendCompact(nil), Redactions: redactor.count, RedactionPaths: redactor.paths,
		Values: values, Depth: depth, TopLevel: jsonType(root),
	}
	if workflows := n8nWorkflows(root); workflows != nil {
		prepared.Format = JSONFormatN8N
		for _, workflow := range workflows {
			prepared.Workflows = append(prepared.Workflows, summarizeWorkflow(workflow))
		}
	} else if root.kind == jsonObject {
		prepared.Members = jsonMembers(root)
	}
	return Result{Format: FormatJSON, Version: extractorVersion, JSON: prepared}
}

// redactor replaces the likely secrets of a parsed file in place.
type redactor struct {
	count int
	paths []redaction
}

func (r *redactor) record(path string, rule redactionRule) {
	r.count++
	if len(r.paths) < maxRedactionPaths {
		r.paths = append(r.paths, redaction{Path: comm.TruncateUTF8(path, 500), Rule: rule})
	}
}

// value redacts v at path: a string field or header value named like a
// credential entirely, and the secrets inside every other string.
func (r *redactor) value(v *jsonValue, path string) {
	switch v.kind {
	case jsonObject:
		// A name and value pair, such as a header or a query parameter, names
		// its value with "name" or "key".
		pair := v.field("name")
		if pair == nil {
			pair = v.field("key")
		}
		headerSecret := pair != nil && pair.kind == jsonString && credentialName(pair.text)
		for index, key := range v.keys {
			// A key is no place for a secret, but one that looks like one is redacted too.
			v.keys[index] = r.secrets(key, path+".(key)")
			child, childPath := v.items[index], jsonPath(path, v.keys[index])
			switch {
			case child.kind == jsonString && credentialName(key) && literalSecret(child.text):
				child.text = redactionMarker
				r.record(childPath, ruleSecretField)
			case child.kind == jsonString && key == "value" && headerSecret && literalSecret(child.text):
				child.text = redactionMarker
				r.record(childPath, ruleSecretPair)
			default:
				r.value(child, childPath)
			}
		}
	case jsonArray:
		for index, item := range v.items {
			r.value(item, path+"["+strconv.Itoa(index)+"]")
		}
	case jsonString:
		v.text = r.secrets(v.text, path)
	case jsonLiteral:
	}
}

// secrets replaces the secrets that the string rules find inside text.
func (r *redactor) secrets(text, path string) string {
	for _, rule := range stringRules {
		text = rule.pattern.ReplaceAllStringFunc(text, func(match string) string {
			groups := rule.pattern.FindStringSubmatchIndex(match)
			start, end := 0, len(match)
			if len(groups) >= 4 && groups[2] >= 0 {
				start, end = groups[2], groups[3]
			}
			if match[start:end] == redactionMarker {
				return match
			}
			r.record(path, rule)
			return match[:start] + redactionMarker + match[end:]
		})
	}
	return text
}

// The words of a name that matches secretName but holds no credential, which
// credentialName compares with the name's whole words, never its substrings:
// a count or a bound of something (maxTokens, passwordMinLength), what kind
// of value or where it is (tokenType, accessTokenUrl), and a pagination or
// anti-forgery token (nextPageToken, syncToken, X-CSRF-Token) that names no
// credential word (pageAccessToken stays a credential).
var (
	boundWords      = []string{"count", "limit", "length", "size", "ttl", "expiry", "expires", "expiration", "max", "maximum", "min", "minimum", "num"}
	headWords       = []string{"type", "url", "uri"}
	paginationWords = []string{"next", "page", "cursor", "continuation", "sync", "resume", "csrf", "xsrf", "xcsrf"}
	credentialWords = []string{"access", "auth", "refresh", "api", "bearer", "private", "secret", "password", "passwd", "pwd", "passphrase", "session", "client", "account"}
)

// credentialName reports a field or header name that holds a credential:
// one secretName matches, unless its words make it a bound, a kind or place
// of value, or a pagination token.
func credentialName(name string) bool {
	if !secretName.MatchString(name) {
		return false
	}
	words := nameWords(name)
	hasAny := func(set []string) bool { return slices.ContainsFunc(words, func(word string) bool { return slices.Contains(set, word) }) }
	switch {
	case len(words) == 0:
		return true
	case hasAny(boundWords), slices.Contains(headWords, words[len(words)-1]):
		return false
	case hasAny(paginationWords) && !hasAny(credentialWords):
		return false
	}
	return true
}

// nameWords splits a field or header name into its lowercase words: at every
// character that is no letter or digit, where a lowercase letter or a digit
// meets an uppercase one (accountPassword), and before the last capital of a
// run that a lowercase letter follows (APIKey is api, key).
func nameWords(name string) []string {
	var words []string
	var word []rune
	flush := func() {
		if len(word) > 0 {
			words, word = append(words, strings.ToLower(string(word))), word[:0]
		}
	}
	runes := []rune(name)
	for index, r := range runes {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			flush()
			continue
		}
		if index > 0 && unicode.IsUpper(r) {
			previous := runes[index-1]
			nextIsLower := index+1 < len(runes) && unicode.IsLower(runes[index+1])
			if unicode.IsLower(previous) || unicode.IsDigit(previous) || unicode.IsUpper(previous) && nextIsLower {
				flush()
			}
		}
		word = append(word, r)
	}
	flush()
	return words
}

// literalSecret reports a value that may hold a credential itself, as the
// inventory judges one: at least eight characters, not the marker, not a
// placeholder and not an n8n expression, which starts with "=" and names a
// value instead of holding it. The string rules still apply to the others.
func literalSecret(value string) bool {
	trimmed := strings.TrimSpace(value)
	return utf8.RuneCountInString(trimmed) >= 8 && trimmed != redactionMarker && !placeholder.MatchString(trimmed) && !strings.HasPrefix(trimmed, "=")
}

// jsonPath is the path of key below the object at path.
func jsonPath(path, key string) string {
	if identifier.MatchString(key) {
		return path + "." + key
	}
	return path + "[" + strconv.Quote(key) + "]"
}

// n8nWorkflows is the workflows of an n8n export: an object with a nodes
// array of objects with string name and type fields, at least one type of an
// n8n package, and a connections object; such a workflow inside
// {"data": ...}; or a non-empty array of those. Anything else is nil.
func n8nWorkflows(root *jsonValue) []*jsonValue {
	workflow := func(v *jsonValue) *jsonValue {
		if v.field("nodes") == nil && v.field("data") != nil {
			v = v.field("data")
		}
		nodes, connections := v.field("nodes"), v.field("connections")
		if nodes == nil || nodes.kind != jsonArray || connections == nil || connections.kind != jsonObject {
			return nil
		}
		packaged := false
		for _, node := range nodes.items {
			name, nodeType := node.field("name"), node.field("type")
			if name == nil || name.kind != jsonString || nodeType == nil || nodeType.kind != jsonString {
				return nil
			}
			packaged = packaged || strings.HasPrefix(nodeType.text, "n8n-nodes-") || strings.HasPrefix(nodeType.text, "@n8n/")
		}
		if !packaged {
			return nil
		}
		return v
	}
	candidates := []*jsonValue{root}
	if root.kind == jsonArray {
		candidates = root.items
	}
	if len(candidates) == 0 {
		return nil
	}
	var workflows []*jsonValue
	for _, candidate := range candidates {
		found := workflow(candidate)
		if found == nil {
			return nil
		}
		workflows = append(workflows, found)
	}
	return workflows
}

// summarizeWorkflow counts a workflow's node types and lists its triggers and
// the credentials its nodes name, each bounded.
func summarizeWorkflow(workflow *jsonValue) Workflow {
	summary := Workflow{Name: comm.TruncateUTF8(workflow.stringField("name"), 200)}
	counts := map[string]int{}
	credentials := map[Credential]bool{}
	for _, node := range workflow.field("nodes").items {
		summary.Nodes++
		nodeType := node.stringField("type")
		counts[nodeType]++
		short := nodeType[strings.LastIndexByte(nodeType, '.')+1:]
		if (strings.HasSuffix(short, "Trigger") || slices.Contains(n8nTriggers, short)) && len(summary.Triggers) < 50 {
			summary.Triggers = append(summary.Triggers, WorkflowNode{Name: comm.TruncateUTF8(node.stringField("name"), 200), Type: nodeType})
		}
		if named := node.field("credentials"); named != nil && named.kind == jsonObject {
			for index, credentialType := range named.keys {
				credentials[Credential{Type: comm.TruncateUTF8(credentialType, 200), Name: comm.TruncateUTF8(named.items[index].stringField("name"), 200)}] = true
			}
		}
	}
	for nodeType, count := range counts {
		summary.NodeTypes = append(summary.NodeTypes, NodeTypeCount{Type: nodeType, Count: count})
	}
	slices.SortFunc(summary.NodeTypes, func(a, b NodeTypeCount) int { return cmp.Or(b.Count-a.Count, strings.Compare(a.Type, b.Type)) })
	summary.NodeTypes = summary.NodeTypes[:min(len(summary.NodeTypes), 100)]
	for credential := range credentials {
		summary.Credentials = append(summary.Credentials, credential)
	}
	slices.SortFunc(summary.Credentials, func(a, b Credential) int {
		return cmp.Or(strings.Compare(a.Type, b.Type), strings.Compare(a.Name, b.Name))
	})
	summary.Credentials = summary.Credentials[:min(len(summary.Credentials), 100)]
	// connections maps each source node to its outputs, each a list of
	// branches, each a list of links.
	for _, outputs := range workflow.field("connections").items {
		for _, branches := range outputs.items {
			for _, links := range branches.items {
				summary.Connections += len(links.items)
			}
		}
	}
	return summary
}

// jsonMembers describes the first 50 members of an object: each key, its
// value's type and, for an array, its length.
func jsonMembers(object *jsonValue) []JSONMember {
	var members []JSONMember
	for index, key := range object.keys[:min(len(object.keys), 50)] {
		member := JSONMember{Key: comm.TruncateUTF8(key, 100), Type: jsonType(object.items[index])}
		if object.items[index].kind == jsonArray {
			member.Length = len(object.items[index].items)
		}
		members = append(members, member)
	}
	return members
}

// jsonType names the JSON type of v.
func jsonType(v *jsonValue) string {
	switch {
	case v.kind == jsonObject:
		return "object"
	case v.kind == jsonArray:
		return "array"
	case v.kind == jsonString:
		return "string"
	case v.text == "true" || v.text == "false":
		return "boolean"
	case v.text == "null":
		return "null"
	default:
		return "number"
	}
}

// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package fileextract

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf16"
)

// TestMain lets the test binary serve as the extractor child: Extract starts
// it with the file-extract argument, as it starts the server binary.
// A copy named failingChild exits with status 3 instead, a child that ends
// without a result.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == Subcommand {
		if filepath.Base(os.Args[0]) == failingChild {
			os.Exit(3)
		}
		os.Exit(ServeChild(os.Args[2:], os.Stdin, os.Stdout))
	}
	os.Exit(m.Run())
}

const failingChild = "failing-extractor"

// corpus runs files through the real child process.
type corpus struct {
	t         *testing.T
	extractor *Extractor
}

func newCorpus(t *testing.T) corpus {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return corpus{t: t, extractor: NewExtractor(executable)}
}

func (c corpus) extract(format Format, input []byte) Result {
	c.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	request := Request{Format: format, MaxBytes: 20 << 20}
	switch format.Kind() {
	case KindImage:
		request.MaxBytes, request.MaxImageEdge = 1<<20, 2000
	case KindJSON:
		request.MaxBytes = MaxJSONBytes
	}
	result, err := c.extractor.Extract(ctx, request, input)
	if err != nil {
		c.t.Fatal(err)
	}
	if result.Format != format || result.Version != extractorVersion {
		c.t.Fatalf("the result names %s %s, not %s %s", result.Format, result.Version, format, extractorVersion)
	}
	return result
}

func (c corpus) refused(format Format, input []byte, code Code) {
	c.t.Helper()
	if result := c.extract(format, input); result.Refusal == nil || result.Refusal.Code != code {
		c.t.Fatalf("a %s file was not refused as %s: %+v", format, code, result)
	}
}

func (c corpus) document(format Format, input []byte) Document {
	c.t.Helper()
	result := c.extract(format, input)
	if result.Refusal != nil || result.Document == nil {
		c.t.Fatalf("a %s document was not read: %+v", format, result.Refusal)
	}
	return *result.Document
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// outline is a section's ID, heading, level and path, without its text.
type outline struct {
	ID, Heading string
	Level       int
	Path        []string
}

func outlineOf(document Document) []outline {
	var out []outline
	for _, section := range document.Sections {
		out = append(out, outline{section.ID, section.Heading, section.Level, section.Path})
	}
	return out
}

func requireOutline(t *testing.T, document Document, want []outline) {
	t.Helper()
	if got := outlineOf(document); !reflect.DeepEqual(got, want) {
		t.Fatalf("outline\n got %+v\nwant %+v", got, want)
	}
}

func requireText(t *testing.T, section Section, parts ...string) {
	t.Helper()
	for _, part := range parts {
		if !strings.Contains(section.Text, part) {
			t.Fatalf("section %s %q lacks %q in %q", section.ID, section.Heading, part, section.Text)
		}
	}
	if strings.ContainsAny(section.Text, "�\x00") {
		t.Fatalf("section %s holds undecoded text: %q", section.ID, section.Text)
	}
}

func TestTextDocumentCorpusIntegration(t *testing.T) {
	c := newCorpus(t)
	markdown := c.document(FormatMarkdown, fixture(t, "headings.md"))
	requireOutline(t, markdown, []outline{
		{"s1", "", 0, []string{}},
		{"s2", "Onboarding guide", 1, []string{"Onboarding guide"}},
		{"s3", "Accounts", 2, []string{"Onboarding guide", "Accounts"}},
		{"s4", "Access", 2, []string{"Onboarding guide", "Access"}},
		{"s5", "Laptop setup", 3, []string{"Onboarding guide", "Access", "Laptop setup"}},
		{"s6", "Second setext title", 1, []string{"Second setext title"}},
	})
	if want := "---\ntitle: Onboarding guide\n---\nThis guide explains how new hires get their accounts."; markdown.Sections[0].Text != want {
		t.Fatalf("the front matter section is %q", markdown.Sections[0].Text)
	}
	if want := "Create the email account first.\n\n```sh\n# not a heading inside code\nmake account\n```"; markdown.Sections[2].Text != want {
		t.Fatalf("the fenced code section is %q", markdown.Sections[2].Text)
	}
	if markdown.Truncated || markdown.TextBytes == 0 {
		t.Fatalf("the Markdown document is truncated %v with %d bytes", markdown.Truncated, markdown.TextBytes)
	}

	// A long section splits into parts that keep its heading.
	long := "# Pricing\n\n" + strings.Repeat("Every plan is billed monthly in advance.\n\n", 2000)
	parts := c.document(FormatMarkdown, []byte(long))
	if len(parts.Sections) < 3 || parts.Sections[1].Heading != "Pricing (part 2)" || parts.Sections[1].Level != 1 {
		t.Fatalf("a long section split into %+v", outlineOf(parts))
	}
	for _, section := range parts.Sections {
		if len(section.Text) > maxSectionBytes {
			t.Fatalf("section %s holds %d bytes", section.ID, len(section.Text))
		}
	}

	// Plain text in UTF-8 with a BOM and in UTF-16 either way reads the same.
	text := "First paragraph about refunds.\r\n\r\nSecond paragraph, café 退款.\r\n"
	utf8Text := c.document(FormatText, append([]byte{0xEF, 0xBB, 0xBF}, text...))
	requireOutline(t, utf8Text, []outline{{"s1", "Part 1", 0, []string{"Part 1"}}})
	if want := "First paragraph about refunds.\n\nSecond paragraph, café 退款."; utf8Text.Sections[0].Text != want {
		t.Fatalf("the text section is %q", utf8Text.Sections[0].Text)
	}
	for _, encoded := range [][]byte{utf16Bytes(text, binary.LittleEndian), utf16Bytes(text, binary.BigEndian)} {
		if got := c.document(FormatText, encoded); !reflect.DeepEqual(got, utf8Text) {
			t.Fatalf("UTF-16 text read %+v, not %+v", got, utf8Text)
		}
	}
	// Text beyond 2 MiB keeps its beginning and is truncated.
	big := c.document(FormatText, []byte(strings.Repeat(strings.Repeat("word ", 400)+"\n\n", 1500)))
	if !big.Truncated || big.TextBytes > maxDocumentText || len(big.Sections) > maxSections {
		t.Fatalf("a 3 MB text kept %d bytes in %d sections, truncated %v", big.TextBytes, len(big.Sections), big.Truncated)
	}

	c.refused(FormatText, []byte("valid start \xff\xfe broken"), CodeUnsupportedContent)
	c.refused(FormatMarkdown, []byte("# Title\x00with a NUL"), CodeUnsupportedContent)
	c.refused(FormatText, []byte{0xFF, 0xFE, 'a', 0, 'b'}, CodeUnsupportedContent)
	c.refused(FormatText, append([]byte{0xFF, 0xFE}, 0x00, 0xD8, 'a', 0), CodeUnsupportedContent)
	c.refused(FormatText, []byte(" \n\n\t\n"), CodeNoText)
	c.refused(FormatMarkdown, bytes.Repeat([]byte("a"), 20<<20+1), CodeTooLarge)
}

func utf16Bytes(text string, order binary.AppendByteOrder) []byte {
	encoded := []byte{0xFF, 0xFE}
	if order == binary.BigEndian {
		encoded = []byte{0xFE, 0xFF}
	}
	for _, unit := range utf16.Encode([]rune(text)) {
		encoded = order.AppendUint16(encoded, unit)
	}
	return encoded
}

// wordStyles names a Title, two heading styles and a custom style based on
// Heading 2 under a localized ID.
const wordStyles = `<?xml version="1.0" encoding="UTF-8"?>
<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
<w:style w:type="paragraph" w:styleId="Normal"><w:name w:val="Normal"/></w:style>
<w:style w:type="paragraph" w:styleId="Title"><w:name w:val="Title"/></w:style>
<w:style w:type="paragraph" w:styleId="Heading1"><w:name w:val="heading 1"/><w:pPr><w:outlineLvl w:val="0"/></w:pPr></w:style>
<w:style w:type="paragraph" w:styleId="berschrift2"><w:name w:val="heading 2"/></w:style>
<w:style w:type="paragraph" w:styleId="PolicySection"><w:name w:val="Policy section"/><w:basedOn w:val="berschrift2"/></w:style>
</w:styles>`

// wordDocument has a title, headings by style, by a based-on style and by an
// outline level, a table, a tracked deletion and insertion, a drawing and a field.
const wordDocument = `<?xml version="1.0" encoding="UTF-8"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:mc="http://schemas.openxmlformats.org/markup-compatibility/2006">
<w:body>
<w:p><w:r><w:t>Before the title.</w:t></w:r></w:p>
<w:p><w:pPr><w:pStyle w:val="Title"/></w:pPr><w:r><w:t>Expense policy</w:t></w:r></w:p>
<w:p><w:r><w:t xml:space="preserve">Submit expenses </w:t></w:r><w:del><w:r><w:delText>never</w:delText></w:r></w:del><w:ins><w:r><w:t>within thirty days</w:t></w:r></w:ins><w:r><w:t>.</w:t></w:r></w:p>
<w:p><w:pPr><w:pStyle w:val="Heading1"/></w:pPr><w:r><w:t>Travel</w:t></w:r></w:p>
<w:tbl><w:tr><w:tc><w:p><w:r><w:t>Item</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>Limit</w:t></w:r></w:p></w:tc></w:tr>
<w:tr><w:tc><w:p><w:r><w:t>Hotel</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>200 USD</w:t></w:r><w:r><w:tab/><w:t>per night</w:t></w:r></w:p></w:tc></w:tr></w:tbl>
<w:p><w:pPr><w:pStyle w:val="PolicySection"/></w:pPr><w:r><w:t>Flights</w:t></w:r></w:p>
<w:p><w:r><w:drawing><w:t>text of a drawing</w:t></w:drawing></w:r><w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText>PAGE</w:instrText></w:r><w:r><w:t>Book economy class.</w:t></w:r></w:p>
<w:p><w:pPr><w:outlineLvl w:val="0"/></w:pPr><w:r><w:t>Meals</w:t></w:r></w:p>
<w:p><w:r><w:t>Up to 60 USD a day. 餐费每天六十美元。</w:t></w:r><w:r><w:br/><w:t>Receipts required.</w:t></w:r></w:p>
</w:body></w:document>`

// zipArchive packs members, compressed.
func zipArchive(t *testing.T, members map[string][]byte) []byte {
	t.Helper()
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	for _, name := range []string{"[Content_Types].xml", "word/document.xml", "word/styles.xml"} {
		if data, found := members[name]; found {
			member, err := writer.Create(name)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = member.Write(data); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return archive.Bytes()
}

func TestWordCorpusIntegration(t *testing.T) {
	c := newCorpus(t)
	styled := c.document(FormatDOCX, zipArchive(t, map[string][]byte{"[Content_Types].xml": []byte("<Types/>"), "word/document.xml": []byte(wordDocument), "word/styles.xml": []byte(wordStyles)}))
	requireOutline(t, styled, []outline{
		{"s1", "", 0, []string{}},
		{"s2", "Expense policy", 1, []string{"Expense policy"}},
		{"s3", "Travel", 1, []string{"Travel"}},
		{"s4", "Flights", 2, []string{"Travel", "Flights"}},
		{"s5", "Meals", 1, []string{"Meals"}},
	})
	for at, want := range []string{
		"Before the title.",
		"Submit expenses within thirty days.",
		"Item\tLimit\nHotel\t200 USD\tper night",
		"Book economy class.",
		"Up to 60 USD a day. 餐费每天六十美元。\nReceipts required.",
	} {
		if styled.Sections[at].Text != want {
			t.Fatalf("section %s is %q, not %q", styled.Sections[at].ID, styled.Sections[at].Text, want)
		}
	}

	// A document another tool wrote without styles is one section of its paragraphs.
	plain := c.document(FormatDOCX, fixture(t, "textutil-no-styles.docx"))
	requireOutline(t, plain, []outline{{"s1", "", 0, []string{}}})
	requireText(t, plain.Sections[0], "Expense policy\nEmployees submit expenses within thirty days.", "Item\nLimit\nHotel\n200 USD")

	// A ZIP bomb: document.xml expands a thousandfold.
	bomb := zipArchive(t, map[string][]byte{"word/document.xml": bytes.Repeat([]byte("a"), 10<<20)})
	if len(bomb) > 100<<10 {
		t.Fatalf("the bomb archive has %d bytes", len(bomb))
	}
	c.refused(FormatDOCX, bomb, CodeUnsupportedContent)
	c.refused(FormatDOCX, zipArchive(t, map[string][]byte{"word/styles.xml": []byte(wordStyles)}), CodeUnsupportedContent)
	c.refused(FormatDOCX, fixture(t, "chromium-outline.pdf"), CodeUnsupportedContent)
	compound := append(append([]byte{}, cfbSignature...), make([]byte, 512)...)
	c.refused(FormatDOCX, compound, CodeUnsupportedContent)
	c.refused(FormatDOCX, append(compound, utf16LE("EncryptionInfo")...), CodeEncrypted)
	nested := zipArchive(t, map[string][]byte{"word/document.xml": []byte(`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
		strings.Repeat("<w:p>", 300) + strings.Repeat("</w:p>", 300) + `</w:document>`)})
	c.refused(FormatDOCX, nested, CodeUnsupportedContent)
}

func TestPDFCorpusIntegration(t *testing.T) {
	c := newCorpus(t)
	// Chromium: an outline of three levels, a table, CJK text in Type3 fonts.
	chromium := c.document(FormatPDF, fixture(t, "chromium-outline.pdf"))
	requireOutline(t, chromium, []outline{
		{"s1", "Refund process", 1, []string{"Refund process"}},
		{"s2", "1. Intake", 2, []string{"Refund process", "1. Intake"}},
		{"s3", "2. Approval", 2, []string{"Refund process", "2. Approval"}},
		{"s4", "2.1 Escalation", 3, []string{"Refund process", "2. Approval", "2.1 Escalation"}},
		{"s5", "3. Payout", 2, []string{"Refund process", "3. Payout"}},
	})
	requireText(t, chromium.Sections[1], "The agent checks the order number", "Field Owner\nOrder number Customer")
	requireText(t, chromium.Sections[4], "café, naïve, 退款流程, emoji ✓.")
	if chromium.PageCount != 3 {
		t.Fatalf("the Chromium PDF has %d pages", chromium.PageCount)
	}

	// Quartz: an outline that PDFKit wrote, ligatures and CJK text in a TrueType subset.
	quartz := c.document(FormatPDF, fixture(t, "quartz-outline.pdf"))
	requireOutline(t, quartz, []outline{
		{"s1", "Vendor onboarding", 1, []string{"Vendor onboarding"}},
		{"s2", "Legal review", 1, []string{"Legal review"}},
		{"s3", "Payment terms", 1, []string{"Payment terms"}},
	})
	requireText(t, quartz.Sections[0], "before the first purchase order. The buyer\nfiles the request")
	requireText(t, quartz.Sections[2], "付款条款需要\n财务总监批准。")

	// Only an owner password restricts it, so it opens; without an outline every page is a section.
	restricted := c.document(FormatPDF, fixture(t, "quartz-owner-password.pdf"))
	requireOutline(t, restricted, []outline{
		{"s1", "Page 1", 0, []string{"Page 1"}}, {"s2", "Page 2", 0, []string{"Page 2"}}, {"s3", "Page 3", 0, []string{"Page 3"}},
	})
	requireText(t, restricted.Sections[1], "Legal review\nLegal reads the master agreement")

	// A header of a version the library does not name opens as well.
	newer := fixture(t, "chromium-outline.pdf")
	copy(newer, "%PDF-2.0")
	if got := c.document(FormatPDF, newer); !reflect.DeepEqual(outlineOf(got), outlineOf(chromium)) {
		t.Fatalf("a PDF 2.0 header read %+v", outlineOf(got))
	}

	c.refused(FormatPDF, fixture(t, "quartz-user-password.pdf"), CodeEncrypted)
	c.refused(FormatPDF, fixture(t, "quartz-image-only.pdf"), CodeNoText)
	truncated := fixture(t, "chromium-outline.pdf")
	c.refused(FormatPDF, truncated[:len(truncated)/2], CodeUnsupportedContent)
	c.refused(FormatPDF, fixture(t, "headings.md"), CodeUnsupportedContent)
}

func TestJSONCorpusIntegration(t *testing.T) {
	c := newCorpus(t)
	export := c.extract(FormatJSON, fixture(t, "n8n-export.json"))
	if export.Refusal != nil || export.JSON == nil {
		t.Fatalf("the n8n export was not prepared: %+v", export.Refusal)
	}
	prepared := *export.JSON
	if want := fixture(t, "n8n-export.redacted.json"); !bytes.Equal(append(append([]byte{}, prepared.Redacted...), '\n'), want) {
		t.Fatalf("the redacted export differs from testdata/n8n-export.redacted.json:\n%s", prepared.Redacted)
	}
	if bytes.Contains(prepared.Redacted, []byte("EXAMPLEQUERYKEY")) || bytes.Contains(prepared.Redacted, []byte("AKIAIOSFODNN7EXAMPLE")) {
		t.Fatal("a secret survived the redaction")
	}
	wantPaths := []redaction{
		{"$.nodes[1].parameters.url", ruleURLQuery},
		{"$.nodes[1].parameters.headerParameters.parameters[0].value", ruleSecretPair},
		{"$.nodes[1].parameters.queryParameters.parameters[0].value", ruleSecretPair},
		{"$.nodes[2].parameters.assignments.assignments[0].value", ruleSecretPair},
		{"$.nodes[2].parameters.privateKey", ruleSecretField},
		{"$.nodes[3].parameters.text", ruleSlackToken},
		{"$.nodes[4].parameters.additionalFields.note", ruleGitHubToken},
		{"$.nodes[4].parameters.additionalFields.note", ruleAWSAccessKey},
		{"$.nodes[4].parameters.additionalFields.callback", ruleURLPassword},
		{"$.nodes[5].parameters.jsCode", rulePrivateKey},
	}
	if prepared.Format != JSONFormatN8N || prepared.Redactions != len(wantPaths) || !reflect.DeepEqual(sortedRedactions(prepared.RedactionPaths), sortedRedactions(wantPaths)) {
		t.Fatalf("the export is %s with %d redactions at %+v", prepared.Format, prepared.Redactions, prepared.RedactionPaths)
	}
	wantWorkflow := Workflow{
		Name: "Lead intake", Nodes: 6, Connections: 5,
		NodeTypes:   []NodeTypeCount{{"n8n-nodes-base.code", 1}, {"n8n-nodes-base.httpRequest", 1}, {"n8n-nodes-base.hubspot", 1}, {"n8n-nodes-base.set", 1}, {"n8n-nodes-base.slack", 1}, {"n8n-nodes-base.webhook", 1}},
		Triggers:    []WorkflowNode{{"Incoming lead", "n8n-nodes-base.webhook"}},
		Credentials: []Credential{{"hubspotApi", "HubSpot"}, {"slackApi", "Sales Slack"}},
	}
	if len(prepared.Workflows) != 1 || !reflect.DeepEqual(prepared.Workflows[0], wantWorkflow) {
		t.Fatalf("the export summary is %+v", prepared.Workflows)
	}

	// Preparing the redacted copy again finds nothing new and changes nothing.
	again := c.extract(FormatJSON, prepared.Redacted)
	if again.JSON == nil || again.JSON.Redactions != 0 || !bytes.Equal(again.JSON.Redacted, prepared.Redacted) {
		t.Fatalf("the redacted export prepared again: %+v", again.JSON)
	}

	// The wrapper of the n8n API and a list of workflows are exports too.
	var workflow any
	if err := json.Unmarshal(prepared.Redacted, &workflow); err != nil {
		t.Fatal(err)
	}
	for _, shape := range []any{map[string]any{"data": workflow}, []any{workflow, workflow}} {
		encoded, err := json.Marshal(shape)
		if err != nil {
			t.Fatal(err)
		}
		if wrapped := c.extract(FormatJSON, encoded); wrapped.JSON == nil || wrapped.JSON.Format != JSONFormatN8N || len(wrapped.JSON.Workflows) == 0 {
			t.Fatalf("a wrapped export was not n8n: %+v", wrapped.JSON)
		}
	}

	generic := c.extract(FormatJSON, fixture(t, "generic.json"))
	if generic.JSON == nil || generic.JSON.Format != JSONFormatGeneric || generic.JSON.Redactions != 7 || generic.JSON.TopLevel != "object" {
		t.Fatalf("the generic file prepared as %+v", generic.JSON)
	}
	if want := []JSONMember{{"service", "string", 0}, {"config", "object", 0}, {"items", "array", 3}, {"enabled", "boolean", 0}, {"note", "null", 0}}; !reflect.DeepEqual(generic.JSON.Members, want) {
		t.Fatalf("the generic members are %+v", generic.JSON.Members)
	}
	// A name's words decide, never its substrings: an account's password or token, and a page's access token, are credentials.
	for _, secret := range []string{"EXAMPLEDBPASSWORD01", "EXAMPLEHOOKTOKEN01", "EXAMPLESIGNATURE0", "EXAMPLEPAIRKEY0001", "EXAMPLEACCOUNTTOKEN1", "EXAMPLEACCOUNTPASS01", "EAAEXAMPLEPAGETOKEN01"} {
		if bytes.Contains(generic.JSON.Redacted, []byte(secret)) {
			t.Fatalf("%s survived the redaction", secret)
		}
	}
	for _, kept := range []string{`"maxTokens": 4096`, "&region=eu", `"tokenUrl": "https://auth.example.org/oauth/token"`, `"nextPageToken": "EXAMPLECURSOR0000000002"`} {
		if !bytes.Contains(generic.JSON.Redacted, []byte(kept)) {
			t.Fatalf("the redaction changed more than secrets, %s is gone:\n%s", kept, generic.JSON.Redacted)
		}
	}

	c.refused(FormatJSON, []byte(`{"nodes": [}`), CodeInvalidJSON)
	c.refused(FormatJSON, []byte(`{"a": 1} {"b": 2}`), CodeInvalidJSON)
	c.refused(FormatJSON, []byte("{\"a\": \"\xff\"}"), CodeInvalidJSON)
	if deep := c.extract(FormatJSON, []byte(strings.Repeat("[", 64)+strings.Repeat("]", 64))); deep.JSON == nil || deep.JSON.Depth != 64 {
		t.Fatalf("a JSON file 64 levels deep prepared as %+v", deep)
	}
	c.refused(FormatJSON, []byte(strings.Repeat("[", 65)+strings.Repeat("]", 65)), CodeTooLarge)
	c.refused(FormatJSON, []byte("["+strings.Repeat("0,", 200_000)+"0]"), CodeTooLarge)
	c.refused(FormatJSON, bytes.Repeat([]byte(" "), MaxJSONBytes+1), CodeTooLarge)
}

func sortedRedactions(redactions []redaction) []string {
	var out []string
	for _, redaction := range redactions {
		out = append(out, redaction.Path+" "+redaction.Rule.String())
	}
	slices.Sort(out)
	return out
}

func encodeImage(t *testing.T, encode func(*bytes.Buffer, image.Image) error, width, height int) []byte {
	t.Helper()
	picture := image.NewRGBA(image.Rect(0, 0, width, height))
	for x := 0; x < width; x++ {
		picture.Set(x, 0, color.RGBA{R: 200, A: 255})
	}
	var encoded bytes.Buffer
	if err := encode(&encoded, picture); err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes()
}

func TestImageProbeCorpusIntegration(t *testing.T) {
	c := newCorpus(t)
	encodePNG := func(out *bytes.Buffer, picture image.Image) error { return png.Encode(out, picture) }
	encodeJPEG := func(out *bytes.Buffer, picture image.Image) error { return jpeg.Encode(out, picture, nil) }
	encodeGIF := func(out *bytes.Buffer, picture image.Image) error { return gif.Encode(out, picture, nil) }
	for _, probe := range []struct {
		format Format
		data   []byte
		want   ImageProbe
	}{
		{FormatPNG, encodeImage(t, encodePNG, 640, 480), ImageProbe{640, 480, 1}},
		{FormatJPEG, encodeImage(t, encodeJPEG, 1999, 20), ImageProbe{1999, 20, 1}},
		{FormatGIF, encodeImage(t, encodeGIF, 32, 16), ImageProbe{32, 16, 1}},
		{FormatWebP, fixture(t, "chromium-lossy.webp"), ImageProbe{48, 32, 1}},
		{FormatWebP, fixture(t, "chromium-lossless.webp"), ImageProbe{48, 32, 1}},
		{FormatGIF, animatedGIF(t), ImageProbe{24, 24, 3}},
		{FormatPNG, animatedPNG(t), ImageProbe{20, 10, 4}},
	} {
		result := c.extract(probe.format, probe.data)
		if result.Refusal != nil || result.Image == nil || *result.Image != probe.want {
			t.Fatalf("the %s probe answered %+v %+v, not %+v", probe.format, result.Image, result.Refusal, probe.want)
		}
	}
	c.refused(FormatJPEG, encodeImage(t, encodePNG, 10, 10), CodeImageMismatch)
	c.refused(FormatPNG, fixture(t, "chromium-lossy.webp"), CodeImageMismatch)
	c.refused(FormatGIF, []byte("not an image at all"), CodeImageMismatch)
	c.refused(FormatPNG, encodeImage(t, encodePNG, 2001, 10), CodeImageTooLarge)
	c.refused(FormatPNG, pngHeader(60_000, 60_000), CodeImageTooLarge)
	c.refused(FormatPNG, bytes.Repeat([]byte{0}, 1<<20+1), CodeTooLarge)
	outside := animatedGIF(t)
	// Move the first frame's left edge beyond the 24 pixel screen.
	binary.LittleEndian.PutUint16(outside[bytes.IndexByte(outside[13:], 0x2C)+13+1:], 30)
	c.refused(FormatGIF, outside, CodeUnsupportedContent)
}

// animatedGIF is a 24x24 GIF of three frames.
func animatedGIF(t *testing.T) []byte {
	t.Helper()
	palette := color.Palette{color.Black, color.White}
	animation := &gif.GIF{}
	for frame := 0; frame < 3; frame++ {
		picture := image.NewPaletted(image.Rect(0, 0, 24, 24), palette)
		picture.SetColorIndex(frame, frame, 1)
		animation.Image, animation.Delay = append(animation.Image, picture), append(animation.Delay, 10)
	}
	var encoded bytes.Buffer
	if err := gif.EncodeAll(&encoded, animation); err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes()
}

// animatedPNG is a PNG whose acTL chunk announces four frames.
func animatedPNG(t *testing.T) []byte {
	t.Helper()
	still := encodeImage(t, func(out *bytes.Buffer, picture image.Image) error { return png.Encode(out, picture) }, 20, 10)
	// The signature and the IHDR chunk (8 + 25 bytes) come first.
	animation := append([]byte{}, still[:33]...)
	animation = append(animation, pngChunk("acTL", binary.BigEndian.AppendUint32(binary.BigEndian.AppendUint32(nil, 4), 0))...)
	return append(animation, still[33:]...)
}

// pngHeader is a PNG of a signature and a header that declares width x height.
func pngHeader(width, height uint32) []byte {
	header := binary.BigEndian.AppendUint32(binary.BigEndian.AppendUint32(nil, width), height)
	header = append(header, 8, 2, 0, 0, 0)
	return append([]byte("\x89PNG\r\n\x1a\n"), pngChunk("IHDR", header)...)
}

func pngChunk(kind string, data []byte) []byte {
	chunk := binary.BigEndian.AppendUint32(nil, uint32(len(data)))
	chunk = append(append(chunk, kind...), data...)
	return binary.BigEndian.AppendUint32(chunk, crc32.ChecksumIEEE(append([]byte(kind), data...)))
}

// TestExtractorFairSlotIntegration runs one queue's backlog of real children
// and, once the first of them finished, one extraction of another queue: the
// slot serves the queues in turn, so the other queue's extraction finishes
// within a few of the backlog's instead of after all of them.
func TestExtractorFairSlotIntegration(t *testing.T) {
	c := newCorpus(t)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	const backlog = 10
	var mu sync.Mutex
	var finished []string
	firstDone := make(chan struct{})
	run := func(queue, name string) {
		result, err := c.extractor.Extract(ctx, Request{Format: FormatMarkdown, MaxBytes: 1 << 20, Queue: queue}, []byte("# "+name+"\n\n"+strings.Repeat("A line of text.\n", 40000)))
		if err != nil || result.Document == nil {
			t.Errorf("the extraction %s failed: %v", name, err)
		}
		mu.Lock()
		defer mu.Unlock()
		if finished = append(finished, name); len(finished) == 1 {
			close(firstDone)
		}
	}
	var wait sync.WaitGroup
	for index := range backlog {
		wait.Go(func() { run("busy", fmt.Sprintf("busy-%d", index)) })
	}
	<-firstDone
	wait.Go(func() { run("other", "other") })
	wait.Wait()
	position := slices.Index(finished, "other")
	if position < 0 || position > 4 {
		t.Fatalf("the other queue's extraction finished at position %d of %d: %v", position, len(finished), finished)
	}
	t.Logf("the other queue's extraction finished at position %d behind a backlog of %d", position, backlog)
}

func TestExtractorOutcomesIntegration(t *testing.T) {
	// A child that exits without a result is extraction_failed, never an error.
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	failingPath := filepath.Join(t.TempDir(), failingChild)
	if err = os.WriteFile(failingPath, binary, 0o700); err != nil {
		t.Fatal(err)
	}
	result, err := NewExtractor(failingPath).Extract(context.Background(), Request{Format: FormatText, MaxBytes: 1 << 20}, []byte("text"))
	if err != nil || result.Refusal == nil || result.Refusal.Code != CodeExtractionFailed || !strings.Contains(result.Refusal.Detail, "status 3") {
		t.Fatalf("a failing child answered %+v, %v", result.Refusal, err)
	}
	// A child that cannot start is an error the caller retries.
	missing := NewExtractor(filepath.Join(t.TempDir(), "absent"))
	if _, err = missing.Extract(context.Background(), Request{Format: FormatText, MaxBytes: 1 << 20}, []byte("text")); err == nil {
		t.Fatal("a child that cannot start answered no error")
	}
	// The slot serializes extractions: a waiter whose context ends gives up.
	c := newCorpus(t)
	if err = c.extractor.slot.acquire(context.Background(), "holder"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, err = c.extractor.Extract(ctx, Request{Format: FormatText, MaxBytes: 1 << 20}, []byte("text")); err == nil {
		t.Fatal("an extraction ran while the slot was held")
	}
	c.extractor.slot.release()
	if result := c.extract(FormatText, []byte("text")); result.Document == nil {
		t.Fatalf("the slot was not free once released: %+v", result.Refusal)
	}
	if formats, ok := FormatOf("text/markdown; charset=utf-8", "Guide.MD"); !ok || formats != FormatMarkdown {
		t.Fatal("FormatOf refused a Markdown file")
	}
	if _, ok := FormatOf("image/png", "diagram.jpg"); ok {
		t.Fatal("FormatOf admitted an extension of another format")
	}
}

// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package fileextract

import (
	"mime"
	"path"
	"slices"
	"strings"

	"github.com/superdurable/superversev2/internal/comm"
)

// extractorVersion names the extractor's rules; a prepared file records it, so a
// change of what the same bytes yield is visible.
const extractorVersion = "fileextract-2"

// Kind is what a format's content is to the model: an image, the text of a
// document or a JSON file. A struct that string literals cannot convert to;
// its values below are persisted by their names.
type Kind struct{ name string }

var (
	KindImage    = Kind{"image"}
	KindDocument = Kind{"document"}
	KindJSON     = Kind{"json"}
)

func (k Kind) String() string               { return k.name }
func (k Kind) MarshalText() ([]byte, error) { return []byte(k.name), nil }
func (k *Kind) UnmarshalText(text []byte) error {
	return comm.UnmarshalName(text, k, Kind.String, KindImage, KindDocument, KindJSON)
}

// Format is one accepted file format with the media type that declares it,
// which its stored objects keep, its kind and the file name extensions that
// declare it. A struct that string literals cannot convert to; its values
// below are persisted by their names.
type Format struct {
	name       string
	mediaType  string
	kind       Kind
	extensions [2]string
}

var (
	FormatMarkdown = Format{"markdown", "text/markdown", KindDocument, [2]string{".md", ".markdown"}}
	FormatText     = Format{"text", "text/plain", KindDocument, [2]string{".txt"}}
	FormatDOCX     = Format{"docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", KindDocument, [2]string{".docx"}}
	FormatPDF      = Format{"pdf", "application/pdf", KindDocument, [2]string{".pdf"}}
	FormatJSON     = Format{"json", comm.MediaTypeJSON, KindJSON, [2]string{".json"}}
	FormatPNG      = Format{"png", "image/png", KindImage, [2]string{".png"}}
	FormatJPEG     = Format{"jpeg", "image/jpeg", KindImage, [2]string{".jpg", ".jpeg"}}
	FormatWebP     = Format{"webp", "image/webp", KindImage, [2]string{".webp"}}
	FormatGIF      = Format{"gif", "image/gif", KindImage, [2]string{".gif"}}
)

var formats = []Format{FormatMarkdown, FormatText, FormatDOCX, FormatPDF, FormatJSON, FormatPNG, FormatJPEG, FormatWebP, FormatGIF}

func (f Format) String() string { return f.name }

// MediaType is the media type that declares the format and that its stored
// objects carry.
func (f Format) MediaType() string { return f.mediaType }

// Kind is what the format's content is to the model.
func (f Format) Kind() Kind { return f.kind }

func (f Format) MarshalText() ([]byte, error) { return []byte(f.name), nil }
func (f *Format) UnmarshalText(text []byte) error {
	return comm.UnmarshalName(text, f, Format.String, formats...)
}

// FormatOf is the format of a file named fileName that its uploader declared
// as mediaType: the media type, without parameters, and the extension must
// both name it.
func FormatOf(mediaType, fileName string) (Format, bool) {
	declared, _, err := mime.ParseMediaType(mediaType)
	extension := strings.ToLower(path.Ext(fileName))
	for _, format := range formats {
		if err == nil && declared == format.mediaType && extension != "" && slices.Contains(format.extensions[:], extension) {
			return format, true
		}
	}
	return Format{}, false
}

// Code is why the extractor refused a file. A struct that string literals
// cannot convert to; its values below are persisted by their names, and Studio
// says each in words.
type Code struct{ name string }

var (
	// CodeUnsupportedContent is content that is not a supported file of its
	// declared type.
	CodeUnsupportedContent = Code{"unsupported_content"}
	// CodeEncrypted is a file that needs a password.
	CodeEncrypted = Code{"encrypted"}
	// CodeNoText is a document without text, such as a scanned PDF.
	CodeNoText = Code{"no_text"}
	// CodeTooLarge is a file beyond its size or structure bounds.
	CodeTooLarge = Code{"too_large"}
	// CodeInvalidJSON is a JSON file that does not parse.
	CodeInvalidJSON = Code{"invalid_json"}
	// CodeImageMismatch is an image of another type than declared.
	CodeImageMismatch = Code{"image_mismatch"}
	// CodeImageTooLarge is an image beyond the dimensions allowed.
	CodeImageTooLarge = Code{"image_too_large"}
	// CodeExtractionFailed is a crash, a process limit or the time limit:
	// the same bytes fail the same way, so it is not retried either.
	CodeExtractionFailed = Code{"extraction_failed"}
)

var codes = []Code{CodeUnsupportedContent, CodeEncrypted, CodeNoText, CodeTooLarge, CodeInvalidJSON, CodeImageMismatch, CodeImageTooLarge, CodeExtractionFailed}

func (c Code) String() string { return c.name }

func (c Code) MarshalText() ([]byte, error) { return []byte(c.name), nil }
func (c *Code) UnmarshalText(text []byte) error {
	return comm.UnmarshalName(text, c, Code.String, codes...)
}

// Refusal is the extractor's decision that a file cannot be prepared, a
// business failure that its owner records and never retries. Detail names the
// cause, never the content.
type Refusal struct {
	Code   Code   `json:"code"`
	Detail string `json:"detail"`
}

// refuse is the result that refuses a file of format.
func refuse(format Format, code Code, detail string) Result {
	return Result{Format: format, Version: extractorVersion, Refusal: &Refusal{Code: code, Detail: comm.TruncateUTF8(detail, 500)}}
}

// Result is one file prepared for the model, or its refusal. Exactly one of
// Refusal, Document, JSON and Image is set, the one of the format's kind.
type Result struct {
	Format   Format        `json:"format"`
	Version  string        `json:"version"`
	Refusal  *Refusal      `json:"refusal,omitempty"`
	Document *Document     `json:"document,omitempty"`
	JSON     *PreparedJSON `json:"json,omitempty"`
	Image    *ImageProbe   `json:"image,omitempty"`
}

// Document is the text of a document in sections: at most 512, each at most
// 32 KiB, at most 2 MiB in all. A longer text keeps its beginning and is
// Truncated.
type Document struct {
	Sections  []Section `json:"sections"`
	Truncated bool      `json:"truncated"`
	// TextBytes is the length of every section's text together.
	TextBytes int `json:"textBytes"`
	// PageCount is the number of pages of a PDF.
	PageCount int `json:"pageCount,omitempty"`
}

// Section is one heading of a document and its text. ID, s1 to s512, names it
// within its document; Path is the chain of headings above it and its own.
// Text before the first heading is a section with an empty heading at level 0;
// a part of a longer section keeps its heading with "(part N)".
type Section struct {
	ID      string   `json:"id"`
	Heading string   `json:"heading"`
	Level   int      `json:"level"`
	Path    []string `json:"path"`
	Text    string   `json:"text"`
}

// ImageProbe is what an image's header says: its dimensions, those of a GIF's
// first frame, and its number of frames, which is more than one for an
// animated GIF.
type ImageProbe struct {
	Width  int `json:"width"`
	Height int `json:"height"`
	Frames int `json:"frames"`
}

// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package fileextract

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
)

// maxImageChunks bounds the chunks, blocks or frames a probe walks.
const maxImageChunks = 10_000

// imageSignatures start the files of each image format.
var imageSignatures = map[Format][]byte{
	FormatPNG:  []byte("\x89PNG\r\n\x1a\n"),
	FormatJPEG: {0xFF, 0xD8, 0xFF},
	FormatGIF:  []byte("GIF8"),
	FormatWebP: []byte("RIFF"),
}

// probeImage reads only the header of an image, never its pixels: the format
// its bytes are must be the declared one, and its dimensions, those of a GIF's
// first frame too, at most maxEdge on the long edge.
func probeImage(format Format, data []byte, maxEdge int) Result {
	for other, signature := range imageSignatures {
		if bytes.HasPrefix(data, signature) && (other != FormatWebP || len(data) >= 12 && string(data[8:12]) == "WEBP") {
			if other != format {
				return refuse(format, CodeImageMismatch, "the image is "+other.name+", not "+format.name)
			}
			probe, err := imageProbes[format](data)
			if err != nil {
				return refuse(format, CodeUnsupportedContent, "the "+format.name+" header is not readable: "+err.Error())
			}
			if probe.Width < 1 || probe.Height < 1 || max(probe.Width, probe.Height) > maxEdge {
				return refuse(format, CodeImageTooLarge, fmt.Sprintf("the image is %dx%d pixels; the long edge may be at most %d", probe.Width, probe.Height, maxEdge))
			}
			return Result{Format: format, Version: extractorVersion, Image: &probe}
		}
	}
	return refuse(format, CodeImageMismatch, "the file is not a "+format.name+" image")
}

// imageProbes read the header of each image format.
var imageProbes = map[Format]func([]byte) (ImageProbe, error){
	FormatPNG:  probePNG,
	FormatJPEG: func(data []byte) (ImageProbe, error) { return configProbe(jpeg.DecodeConfig(bytes.NewReader(data))) },
	FormatGIF:  probeGIF,
	FormatWebP: probeWebP,
}

func configProbe(config image.Config, err error) (ImageProbe, error) {
	return ImageProbe{Width: config.Width, Height: config.Height, Frames: 1}, err
}

// probePNG reads the header and, for an animated PNG, the frame count of its
// acTL chunk, which comes before the image data.
func probePNG(data []byte) (ImageProbe, error) {
	probe, err := configProbe(png.DecodeConfig(bytes.NewReader(data)))
	if err != nil {
		return probe, err
	}
	for offset, chunk := 8, 0; offset+8 <= len(data) && chunk < maxImageChunks; chunk++ {
		length, kind := int(binary.BigEndian.Uint32(data[offset:])), string(data[offset+4:offset+8])
		if kind == "IDAT" || length < 0 || offset+12+length > len(data) {
			break
		}
		if kind == "acTL" && length >= 4 {
			probe.Frames = max(1, int(binary.BigEndian.Uint32(data[offset+8:])))
		}
		offset += 12 + length
	}
	return probe, nil
}

// probeGIF reads the logical screen and walks the blocks without decoding
// them: it counts the frames and requires the first one inside the screen.
func probeGIF(data []byte) (ImageProbe, error) {
	probe, err := configProbe(gif.DecodeConfig(bytes.NewReader(data)))
	if err != nil {
		return probe, err
	}
	offset := 13
	if flags := data[10]; flags&0x80 != 0 {
		offset += 3 << (flags&0x07 + 1)
	}
	probe.Frames = 0
	skipSubBlocks := func() bool {
		for blocks := 0; offset < len(data) && blocks < maxImageChunks*10; blocks++ {
			size := int(data[offset])
			offset += 1 + size
			if size == 0 {
				return true
			}
		}
		return false
	}
	for block := 0; offset < len(data) && block < maxImageChunks; block++ {
		switch data[offset] {
		case 0x21:
			offset += 2
		case 0x2C:
			if offset+10 > len(data) {
				return probe, fmt.Errorf("the image descriptor at %d is cut off", offset)
			}
			left, top := int(binary.LittleEndian.Uint16(data[offset+1:])), int(binary.LittleEndian.Uint16(data[offset+3:]))
			width, height := int(binary.LittleEndian.Uint16(data[offset+5:])), int(binary.LittleEndian.Uint16(data[offset+7:]))
			if probe.Frames == 0 && (left+width > probe.Width || top+height > probe.Height) {
				return probe, fmt.Errorf("the first frame (%dx%d at %d,%d) lies outside the %dx%d screen", width, height, left, top, probe.Width, probe.Height)
			}
			probe.Frames++
			flags := data[offset+9]
			offset += 10
			if flags&0x80 != 0 {
				offset += 3 << (flags&0x07 + 1)
			}
			// The LZW minimum code size precedes the image data.
			offset++
		case 0x3B:
			if probe.Frames == 0 {
				return probe, fmt.Errorf("the image has no frame")
			}
			return probe, nil
		default:
			return probe, fmt.Errorf("unknown block 0x%02x at %d", data[offset], offset)
		}
		if !skipSubBlocks() {
			return probe, fmt.Errorf("a block at %d is cut off", offset)
		}
	}
	if probe.Frames == 0 {
		return probe, fmt.Errorf("the image has no frame")
	}
	return probe, nil
}

// probeWebP reads the first chunk of a WebP file, VP8, VP8L or VP8X, and for
// an animated VP8X counts its ANMF frames.
func probeWebP(data []byte) (ImageProbe, error) {
	if len(data) < 30 {
		return ImageProbe{}, fmt.Errorf("%d bytes are too short for a WebP header", len(data))
	}
	payload := data[20:]
	switch kind := string(data[12:16]); kind {
	case "VP8 ":
		if !bytes.Equal(payload[3:6], []byte{0x9D, 0x01, 0x2A}) {
			return ImageProbe{}, fmt.Errorf("the VP8 frame has no start code")
		}
		return ImageProbe{Width: int(binary.LittleEndian.Uint16(payload[6:]) & 0x3FFF), Height: int(binary.LittleEndian.Uint16(payload[8:]) & 0x3FFF), Frames: 1}, nil
	case "VP8L":
		if payload[0] != 0x2F {
			return ImageProbe{}, fmt.Errorf("the VP8L stream has no signature")
		}
		bits := binary.LittleEndian.Uint32(payload[1:])
		return ImageProbe{Width: int(bits&0x3FFF) + 1, Height: int(bits>>14&0x3FFF) + 1, Frames: 1}, nil
	case "VP8X":
		probe := ImageProbe{Width: int(uint32(payload[4])|uint32(payload[5])<<8|uint32(payload[6])<<16) + 1,
			Height: int(uint32(payload[7])|uint32(payload[8])<<8|uint32(payload[9])<<16) + 1, Frames: 1}
		if payload[0]&0x02 == 0 {
			return probe, nil
		}
		probe.Frames = 0
		for offset, chunk := 12, 0; offset+8 <= len(data) && chunk < maxImageChunks; chunk++ {
			length := int(binary.LittleEndian.Uint32(data[offset+4:]))
			if string(data[offset:offset+4]) == "ANMF" {
				probe.Frames++
			}
			if length < 0 || length > len(data) {
				break
			}
			offset += 8 + length + length%2
		}
		return probe, nil
	default:
		return ImageProbe{}, fmt.Errorf("the first chunk %q is not VP8, VP8L or VP8X", kind)
	}
}

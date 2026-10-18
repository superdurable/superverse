// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { ATTACHMENT_LIMITS, imageFormat, nameWithExtension, type AttachmentFormat } from "./attachment-files";

/*
 * What the browser does to a file before it uploads it: an image is normalized to the image limits, and the
 * bytes it will PUT are hashed, since storage checks the declared SHA-256.
 */

/** The bytes Studio uploads for an image, with the name and media type that match them. */
export interface PreparedImage {
  blob: Blob;
  name: string;
  mediaType: string;
  width: number;
  height: number;
}

/** The lowercase hex SHA-256 of the bytes, by WebCrypto. */
export async function sha256Hex(blob: Blob): Promise<string> {
  const digest = await crypto.subtle.digest("SHA-256", await blob.arrayBuffer());
  return Array.from(new Uint8Array(digest), (byte) => byte.toString(16).padStart(2, "0")).join("");
}

/** The image could not be decoded, or no encoding fits the limits: the file is refused before upload. */
export class ImageNotUsable extends Error {
  constructor(readonly tooLarge: boolean) {
    super(tooLarge ? "The image does not fit the size limit" : "The image could not be decoded");
    this.name = "ImageNotUsable";
  }
}

/** The smallest long edge Studio scales an image down to before it gives up on the size limit. */
const SMALLEST_EDGE = 256;
const QUALITIES = [0.9, 0.8, 0.7, 0.6];

/** The encodings tried for an image, in order: lossless PNG for a PNG or GIF, then lossy WebP or JPEG. */
function encodings(source: AttachmentFormat): { mediaType: string; quality?: number }[] {
  const lossless =
    source.mediaType === "image/png" || source.mediaType === "image/gif" ? [{ mediaType: "image/png" }] : [];
  const lossy = source.mediaType === "image/webp" ? "image/webp" : "image/jpeg";
  return [
    ...lossless,
    ...QUALITIES.map((quality) => ({ mediaType: lossy, quality })),
    ...(lossy === "image/jpeg" ? [] : QUALITIES.map((quality) => ({ mediaType: "image/jpeg", quality }))),
  ];
}

function canvasOf(width: number, height: number): HTMLCanvasElement {
  const canvas = document.createElement("canvas");
  canvas.width = width;
  canvas.height = height;
  return canvas;
}

function encode(canvas: HTMLCanvasElement, mediaType: string, quality?: number): Promise<Blob | null> {
  return new Promise((resolve) => canvas.toBlob(resolve, mediaType, quality));
}

/**
 * The image as Studio uploads it. A PNG, JPEG or WebP within the long edge and the byte limit is uploaded as
 * it is. Any other image, and every GIF (its first frame), is drawn scaled to the long edge, then encoded as
 * PNG when it was a PNG or GIF and that fits, else as WebP (for a WebP) or JPEG at decreasing quality; still
 * too large, it is scaled down further. A browser that cannot encode a type returns PNG, which is skipped.
 */
export async function normalizeImage(file: File, format: AttachmentFormat): Promise<PreparedImage> {
  let bitmap: ImageBitmap;
  try {
    bitmap = await createImageBitmap(file);
  } catch {
    throw new ImageNotUsable(false);
  }
  try {
    const { width, height } = bitmap;
    const longEdge = Math.max(width, height);
    if (
      format.mediaType !== "image/gif" &&
      longEdge <= ATTACHMENT_LIMITS.imageLongEdge &&
      file.size <= ATTACHMENT_LIMITS.imageBytes
    )
      return { blob: file, name: file.name, mediaType: format.mediaType, width, height };
    for (let edge = Math.min(longEdge, ATTACHMENT_LIMITS.imageLongEdge); edge >= SMALLEST_EDGE; edge *= 0.75) {
      const scale = edge / longEdge;
      const canvas = canvasOf(Math.max(1, Math.round(width * scale)), Math.max(1, Math.round(height * scale)));
      const context = canvas.getContext("2d");
      if (!context) throw new ImageNotUsable(false);
      for (const { mediaType, quality } of encodings(format)) {
        // JPEG has no transparency: transparent pixels become white rather than black.
        context.clearRect(0, 0, canvas.width, canvas.height);
        if (mediaType === "image/jpeg") {
          context.fillStyle = "#ffffff";
          context.fillRect(0, 0, canvas.width, canvas.height);
        }
        context.drawImage(bitmap, 0, 0, canvas.width, canvas.height);
        const blob = await encode(canvas, mediaType, quality);
        if (!blob || blob.type !== mediaType || blob.size > ATTACHMENT_LIMITS.imageBytes) continue;
        const encoded = imageFormat(mediaType);
        return {
          blob,
          name: nameWithExtension(file.name, encoded),
          mediaType,
          width: canvas.width,
          height: canvas.height,
        };
      }
    }
    throw new ImageNotUsable(true);
  } finally {
    bitmap.close();
  }
}

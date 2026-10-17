// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

/*
 * The example search, the TypeScript port of internal/dexai/app/apptemplate/example_search.go: the same
 * words, stems, stop words and synonyms, and the same sum of the generated BM25 postings, so the grid ranks
 * the examples exactly as the server shortlists them. scripts/check-example-search.mjs proves the two agree
 * on the labelled requests. It imports nothing, so that Node runs it from that check as it is.
 */

/** One example as the start page shows it, in catalog order. */
interface SearchExample {
  id: string;
  title: string;
  category: string;
  apps: readonly string[];
  decider: string;
  description: string;
}

/** A synonym group: each occurrence of one of its phrases, as stems, also counts as `term`. */
interface SynonymGroup {
  term: string;
  phrases: readonly (readonly string[])[];
}

/** The generated index: the examples, the vocabulary and each term's postings, [position, thousandths]. */
export interface ExampleSearchIndex {
  examples: readonly SearchExample[];
  stopWords: ReadonlySet<string>;
  synonyms: readonly SynonymGroup[];
  postings: ReadonlyMap<string, readonly (readonly [number, number])[]>;
}

/** How many examples the start page's grid shows while the person types. */
export const CLOSEST_SHOWN = 6;

/** The search reads the first 8,000 code points of a text. */
const MAXIMUM_SEARCHED_CODE_POINTS = 8000;
/** A word shorter than this is no search term. */
const MINIMUM_WORD_LENGTH = 2;
/** An ending cut leaves at least this many letters. */
const MINIMUM_STEM_LENGTH = 4;
/** Cut from a word, longest first, until none applies; "ies" becomes "y". */
const STEM_ENDINGS = ["ing", "ies", "ed", "es", "al", "s", "e"] as const;

const isRecord = (value: unknown): value is Record<string, unknown> =>
  typeof value === "object" && value !== null && !Array.isArray(value);
const isStrings = (value: unknown): value is string[] =>
  Array.isArray(value) && value.every((item) => typeof item === "string");

function invalid(): never {
  throw new Error("The generated example search index is not valid; run make generate-app-template");
}

/** Reads the generated example-search.json, which the server embeds byte for byte. */
export function parseExampleSearchIndex(value: unknown): ExampleSearchIndex {
  if (!isRecord(value) || value.schemaVersion !== 1 || !Array.isArray(value.examples)) invalid();
  const examples = value.examples.map((example: unknown): SearchExample => {
    if (!isRecord(example) || !isStrings(example.apps)) invalid();
    const { id, title, category, decider, description } = example;
    if ([id, title, category, decider, description].some((field) => typeof field !== "string")) invalid();
    return {
      id: String(id),
      title: String(title),
      category: String(category),
      apps: example.apps,
      decider: String(decider),
      description: String(description),
    };
  });
  if (!isStrings(value.stopWords) || !Array.isArray(value.synonyms) || !isRecord(value.postings)) invalid();
  const synonyms = value.synonyms.map((group: unknown): SynonymGroup => {
    if (!isRecord(group) || typeof group.term !== "string" || !Array.isArray(group.phrases)) invalid();
    if (!group.phrases.every(isStrings)) invalid();
    return { term: group.term, phrases: group.phrases };
  });
  const postings = new Map<string, (readonly [number, number])[]>();
  for (const [term, list] of Object.entries(value.postings)) {
    if (!Array.isArray(list)) invalid();
    postings.set(
      term,
      list.map((posting: unknown): readonly [number, number] => {
        if (!Array.isArray(posting) || posting.length !== 2) invalid();
        const [position, score] = posting as unknown[];
        if (typeof position !== "number" || typeof score !== "number" || !examples[position]) invalid();
        return [position, score];
      }),
    );
  }
  return { examples, stopWords: new Set(value.stopWords), synonyms, postings };
}

/** The lower-cased ASCII letter and digit runs of a text's first code points; any other character separates. */
function searchWords(text: string): string[] {
  const words: string[] = [];
  let word = "";
  let count = 0;
  for (const character of text) {
    if (++count > MAXIMUM_SEARCHED_CODE_POINTS) break;
    let code = character.codePointAt(0) ?? 0;
    if (code >= 65 && code <= 90) code += 32;
    if ((code >= 97 && code <= 122) || (code >= 48 && code <= 57)) {
      word += String.fromCharCode(code);
      continue;
    }
    if (word) words.push(word);
    word = "";
  }
  if (word) words.push(word);
  return words;
}

/** Cuts endings from a word until none applies with enough letters left. */
function stem(word: string): string {
  let current = word;
  for (let cut = true; cut;) {
    cut = false;
    for (const ending of STEM_ENDINGS)
      if (current.endsWith(ending) && current.length - ending.length >= MINIMUM_STEM_LENGTH) {
        current = current.slice(0, -ending.length) + (ending === "ies" ? "y" : "");
        cut = true;
        break;
      }
  }
  return current;
}

/** The search terms of a text in order: stems of the words that are no stop words, and synonym terms. */
export function searchTerms(index: ExampleSearchIndex, text: string): string[] {
  const words = searchWords(text);
  const stems = words.map(stem);
  const terms: string[] = [];
  words.forEach((word, at) => {
    if (word.length >= MINIMUM_WORD_LENGTH && !index.stopWords.has(word)) terms.push(stems[at]);
    for (const { term, phrases } of index.synonyms)
      for (const phrase of phrases)
        if (
          !(phrase.length === 1 && phrase[0] === term) &&
          at + phrase.length <= stems.length &&
          phrase.every((part, offset) => stems[at + offset] === part)
        )
          terms.push(term);
  });
  return terms;
}

/**
 * The positions of the examples closest to a text, best first and at most `limit`: the sum of the postings of
 * its distinct terms, ties in catalog order. An example that shares no term is not among them.
 */
export function closestPositions(index: ExampleSearchIndex, text: string, limit: number): number[] {
  const scores = new Array<number>(index.examples.length).fill(0);
  const positions: number[] = [];
  for (const term of new Set(searchTerms(index, text)))
    for (const [position, score] of index.postings.get(term) ?? []) {
      if (scores[position] === 0) positions.push(position);
      scores[position] += score;
    }
  return positions.sort((left, right) => scores[right] - scores[left] || left - right).slice(0, limit);
}

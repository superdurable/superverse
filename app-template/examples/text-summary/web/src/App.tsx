import { useEffect, useRef, useState, type FormEvent } from 'react';
import { getTextSummary, startTextSummary } from './api/generated/sdk.gen';
import type { TextSummary } from './api/generated/types.gen';

const pollIntervalMs = 1000;
// The model Step times out after 900 seconds; stop polling shortly after.
const pollLimitMs = 16 * 60 * 1000;
const inProgress = new Set<TextSummary['status']>(['pending', 'generating']);

export function App() {
  const [text, setText] = useState('');
  const [summary, setSummary] = useState<TextSummary | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState('');
  // Each submission replaces the previous one; late responses for older ones are ignored.
  const current = useRef<AbortController | null>(null);

  useEffect(() => () => current.current?.abort(), []);

  async function summarize(event: FormEvent) {
    event.preventDefault();
    current.current?.abort();
    const controller = new AbortController();
    current.current = controller;
    setSubmitting(true);
    setSummary(null);
    setError('');
    try {
      const requestId = crypto.randomUUID();
      const started = await startTextSummary({ body: { requestId, text }, signal: controller.signal });
      if (!started.data) throw new Error(started.error?.message ?? 'The summary could not be started.');
      const deadline = Date.now() + pollLimitMs;
      for (;;) {
        const response = await getTextSummary({ path: { summaryId: started.data.summaryId }, signal: controller.signal });
        if (controller.signal.aborted) return;
        if (!response.data) throw new Error(response.error?.message ?? 'The summary could not be read.');
        setSummary(response.data);
        if (!inProgress.has(response.data.status)) break;
        if (Date.now() > deadline) throw new Error('The summary is still running. Check the run in Studio.');
        await new Promise((resolve) => setTimeout(resolve, pollIntervalMs));
      }
    } catch (failure) {
      if (!controller.signal.aborted) setError(failure instanceof Error ? failure.message : 'The summary failed.');
    } finally {
      if (current.current === controller) setSubmitting(false);
    }
  }

  return (
    <main>
      <h1>Text Summary</h1>
      <form onSubmit={summarize}>
        <label htmlFor="source-text">Text to summarize</label>
        <textarea id="source-text" rows={8} maxLength={65536} required value={text} onChange={(event) => setText(event.target.value)} />
        <button type="submit" disabled={submitting || text.trim() === ''}>{submitting ? 'Summarizing…' : 'Summarize'}</button>
      </form>
      <section aria-live="polite">
        {summary && inProgress.has(summary.status) && <p>Generating the summary…</p>}
        {summary && !inProgress.has(summary.status) && (
          <>
            {summary.summary && <p className="summary">{summary.summary}</p>}
            {summary.notice && <p role="alert">{summary.notice}</p>}
            <dl>
              <dt>Status</dt><dd>{summary.status}</dd>
              <dt>Model</dt><dd>{summary.model || 'Not reported'}</dd>
              <dt>Tokens</dt><dd>{summary.inputTokens} input, {summary.outputTokens} output, {summary.totalTokens} total</dd>
            </dl>
          </>
        )}
        {error && <p role="alert">{error}</p>}
      </section>
    </main>
  );
}

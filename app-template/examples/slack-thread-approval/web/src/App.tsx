import { useEffect, useRef, useState, type FormEvent } from 'react';
import { getThreadApproval } from './api/generated/sdk.gen';
import type { ThreadApproval } from './api/generated/types.gen';

export function App() {
  const [approvalId, setApprovalId] = useState('');
  const [approval, setApproval] = useState<ThreadApproval | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  // Each lookup replaces the previous one; late responses for older ones are ignored.
  const current = useRef<AbortController | null>(null);

  useEffect(() => () => current.current?.abort(), []);

  async function lookUp(event: FormEvent) {
    event.preventDefault();
    current.current?.abort();
    const controller = new AbortController();
    current.current = controller;
    setLoading(true);
    setApproval(null);
    setError('');
    try {
      const response = await getThreadApproval({ path: { approvalId: approvalId.trim() }, signal: controller.signal });
      if (controller.signal.aborted) return;
      if (!response.data) throw new Error(response.error?.message ?? 'The thread approval could not be read.');
      setApproval(response.data);
    } catch (failure) {
      if (!controller.signal.aborted) setError(failure instanceof Error ? failure.message : 'The lookup failed.');
    } finally {
      if (current.current === controller) setLoading(false);
    }
  }

  return (
    <main>
      <h1>Slack Thread Approval</h1>
      <p>
        A new message in the configured Slack channel starts an approval. The first matching reply in its thread
        finishes it: the application answers the thread with “Processing complete.”
      </p>
      <form onSubmit={lookUp}>
        <label htmlFor="approval-id">Approval ID (the run's Flow ID in Studio)</label>
        <input id="approval-id" required value={approvalId} onChange={(event) => setApprovalId(event.target.value)} placeholder="slack-thread-approval-T…-C…-1700000000.000100" />
        <button type="submit" disabled={loading || approvalId.trim() === ''}>{loading ? 'Reading…' : 'Look up'}</button>
      </form>
      <section aria-live="polite">
        {approval && (
          <>
            {approval.notice && <p role="alert">{approval.notice}</p>}
            <dl>
              <dt>Status</dt><dd>{approval.status}</dd>
              <dt>Channel</dt><dd>{approval.channelId || 'Not recorded yet'}</dd>
              <dt>Thread</dt><dd>{approval.threadTimestamp || 'Not recorded yet'}</dd>
              <dt>Replied by</dt><dd>{approval.replyUserId || 'No reply yet'}</dd>
            </dl>
          </>
        )}
        {error && <p role="alert">{error}</p>}
      </section>
    </main>
  );
}

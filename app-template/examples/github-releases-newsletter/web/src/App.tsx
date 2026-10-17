import { useEffect, useRef, useState, type FormEvent } from 'react';
import { subscribe } from './api/generated/sdk.gen';

export function App() {
  const [email, setEmail] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [message, setMessage] = useState('');
  const [error, setError] = useState('');
  const current = useRef<AbortController | null>(null);

  useEffect(() => () => current.current?.abort(), []);

  async function submit(event: FormEvent) {
    event.preventDefault();
    current.current?.abort();
    const controller = new AbortController();
    current.current = controller;
    setSubmitting(true);
    setMessage('');
    setError('');
    try {
      const result = await subscribe({
        body: { requestId: crypto.randomUUID(), email: email.trim() },
        signal: controller.signal,
      });
      if (controller.signal.aborted) return;
      if (!result.data) throw new Error(result.error?.message ?? 'Your subscription could not be saved.');
      setMessage("You're on the list. Watch your inbox for the next release report.");
      setEmail('');
    } catch (failure) {
      if (!controller.signal.aborted) setError(failure instanceof Error ? failure.message : 'Your subscription could not be saved.');
    } finally {
      if (current.current === controller) setSubmitting(false);
    }
  }

  return (
    <main className="page">
      <section className="hero" aria-labelledby="title">
        <p className="eyebrow">SUPER DURABLE · RELEASE NOTES</p>
        <h1 id="title">The week in Dex, delivered.</h1>
        <p className="intro">A concise digest of releases from <strong>Dex</strong> and the <strong>Dex Connectors Library</strong>, sent when our team publishes a new report.</p>
        <form onSubmit={submit}>
          <label htmlFor="subscriber-email">Email address</label>
          <div className="form-row">
            <input id="subscriber-email" type="email" autoComplete="email" placeholder="you@example.com" maxLength={254} required value={email} onChange={(event) => setEmail(event.target.value)} />
            <button type="submit" disabled={submitting}>{submitting ? 'Subscribing…' : 'Subscribe'}</button>
          </div>
          <p className="form-hint">One email per subscriber. We only send the release newsletter.</p>
        </form>
        <div className="feedback" aria-live="polite">
          {message && <p className="success">{message}</p>}
          {error && <p role="alert">{error}</p>}
        </div>
      </section>
      <aside className="about">
        <span className="about-kicker">WHAT YOU'LL GET</span>
        <h2>Only what shipped.</h2>
        <p>Each issue looks back seven days across both repositories, explains the releases in plain language, and links to the original notes.</p>
        <div className="sources">
          <span>01 <strong>superdurable/dex</strong></span>
          <span>02 <strong>superdurable/dex-connectors-library</strong></span>
        </div>
      </aside>
    </main>
  );
}

# Reviewed published dependency inputs

`dex-1.13.0-llm-0.2.0.json` records the formal Dex Server v1.13.0 multi-platform
image index (`sha256:5a1e81eb…1034d`, inspected in the public registry on
2026-10-21), the bp1 build profile identity and the published LLM Connector v0.2.0
release metadata. Dex Server is backward compatible with every earlier SDK
release, so the platform keeps this image at the newest stable release. It
supersedes `dex-1.11.0-llm-0.2.0.json` (`sha256:d3763711…02fdc9`), which superseded `dex-1.10.1-llm-0.2.0.json`, which used
`sha256:5772a525…ca2872`; historical receipts retain their recorded identities.

Each file selects the dependencies for hosted-eligible Kind validation. It is
not a passed Kind attestation or permission to skip any real business case.
Follow the manual test release runbook
for full Local Kind acceptance before AWS publication. Local source overlays
and Kind-only image aliases remain ineligible for promotion.

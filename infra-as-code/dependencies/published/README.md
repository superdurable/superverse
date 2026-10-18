# Reviewed published dependency inputs

`dex-1.11.0-llm-0.2.0.json` records the formal Dex Server v1.11.0 multi-platform
image index (`sha256:d3763711…02fdc9`, inspected in the public registry on
2026-10-16), the bp1 build profile identity and the published LLM Connector v0.2.0
release metadata. Dex Server is backward compatible with every earlier SDK
release, so the platform keeps this image at the newest stable release. It
replaced `dex-1.10.1-llm-0.2.0.json`, which differed only in the Dex image
(`sha256:5772a525…ca2872`). That file replaced `dex-1.5.0-llm-0.2.0.json`, which differed only in the Dex image
(`sha256:7254c796…aa47d`). The previous file replaced `dex-1.4.1-llm-0.2.0.json`,
which differed only in the Dex image (`sha256:c25e451c…5fca5`). That file replaced
`dex-1.3.0-llm-0.2.0.json` when the build profiles dropped their inert template/skill fields (pre-launch; earlier
receipts keep the recorded identities of both). The bp1 identity was refreshed in
place, also before launch, when the never-executed publisher script left the
profile.

Each file selects the dependencies for hosted-eligible Kind validation. It is
not a passed Kind attestation or permission to skip any real business case.
Follow the manual test release runbook
for full Local Kind acceptance before AWS publication. Local source overlays
and Kind-only image aliases remain ineligible for promotion.

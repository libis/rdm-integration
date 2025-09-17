# @ai-tool: Copilot

## Summary

- Annotate Dataverse citation fields with a `source` string (e.g., `codemeta.json`, `CITATION.cff`) purely for UI display.
- No change to submission payload or templates; unknown props ignored by consumers.
- Built and unit tests PASS locally via `make`.

## AI Provenance (required for AI-assisted changes)

- Prompt: Add Source column in UI; backend should annotate metadata with provenance per field.
- Model: GitHub Copilot gpt-5
- Date: 2025-09-16T10:00:00Z
- Author: @ErykKul
- Role: deployer

## Compliance checklist

- [x] No secrets/PII
- [ ] Transparency notice updated (if user-facing)
- [x] Agent logging enabled (actions/decisions logged)
- [x] Kill-switch / feature flag present for AI features
- [x] No prohibited practices under EU AI Act
- [x] Human oversight retained (required if high-risk or agent mode)
Risk classification: limited
Personal data: no
DPIA: N/A
Automated decision-making: no
Agent mode used: yes
GPAI obligations: N/A
Vendor GPAI compliance reviewed: N/A
- [x] License/IP attestation
Attribution: N/A

### Change-type specifics

- Security review: N/A
- Backend/API changed:
	- ASVS: N/A
- Log retention policy: N/A

## Tests & Risk

- [x] Unit/integration tests added/updated
- [x] Security scan passed
Rollback plan: Revert PR
Smoke test: N/A
- [x] Docs updated (if needed)

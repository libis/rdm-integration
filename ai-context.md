<!--
@ai-context: true
@template_source: https://github.com/libis/ai-transition/blob/main/templates/pilot-starter/ai-context.md
@org_repo: https://github.com/libis/ai-transition
@version: 0.6.0
@workflows_source_api: https://api.github.com/repos/libis/ai-transition/contents/.github?ref=main
@last_context_update: 2025-09-12T16:28:00Z
@last_github_sync: 1970-01-01T00:00:00Z
@upstream_ref: main
@suggestions_enabled: true
@suggestions_policy: after_user_reads_context
@notes: Only surface suggestions after the user explicitly asks to "read context". Respect per-user opt-out via .ai/context.local.json.
-->

# AI Context — Reusable Governance (Pilot/Production)

Purpose

- Provide a minimal, reusable governance context for any repo.
- Bootstrap with org-standard .github workflows and PR template from this project: https://github.com/libis/ai-transition
- Keep itself and workflows up to date using timestamps and a simple verify step.
- Capture provenance and propose context updates only after the user reads this context.

Scope

- Safe to use in pilots and production.
- No repo-local CI logic beyond a small consumer workflow that reuses org workflows.
- Excludes Annex III high-risk domains unless explicitly escalated.

## Commands the agent must support

Important: Act only after the user invokes these commands. Before making changes, enumerate planned edits and ask for confirmation.

2. verify

- Goal: Check if this context and .github are up to date.
- Steps:
  - Compare this file’s @version/@last_context_update to upstream at @template_source.
  - For .github, compare local files to upstream names and checksums using the Content API `sha` fields.
  - Summarize:

    - Context: up-to-date | behind (show upstream version)
    - .github: in sync | drifted | unknown (show planned changes)

  - If drifted, offer “update_workflows”.

    - After running, update `last_verified_at` (UTC ISO-8601) in the State block.

3. update_workflows

- Goal: Sync .github from the upstream project, preserving local customizations.
- Steps:
  - Show planned adds/modifies/removes for .github.
  - For modified files, show a 3-way diff proposal.
  - After confirmation, apply changes, update .github/ai-transition-sync.json and @last_github_sync.
  - Open PR: "chore(ai): sync governance workflows".

    - Use the open_pr command described below to standardize PR creation via GitHub CLI.

4. log_provenance

- Goal: Add AI-Assistance provenance to the PR body.
- Insert if missing:
  - AI-Assistance Provenance

    - Prompt: <summary or link>
    - Model: <model+version>
    - Date: <UTC ISO-8601>
    - Author: @<github-handle>
    - Reviewer confirms: [ ] No secrets/PII; [ ] Licensing respected
    - Notes: <optional>

5. open_pr

- Goal: Create a PR by committing to a new branch and invoking the GitHub CLI using the repo’s PR template at .github/pull_request_template.md.
- Inputs (recommended):
  - title (string), default: context-specific e.g., "chore(ai): bootstrap governance workflows and PR template"
  - branch (string), default: auto-generate e.g., "ai/bootstrap-<YYYYMMDD>"
  - base (string), default: detect repo default branch (fallback: main)
  - labels (array), default: ["ai", "governance"]
  - draft (bool), default: true
  - reviewers (array of handles), optional
  - body_append (string), optional extra notes (e.g., provenance)
- Steps:
  1. Verify prerequisites:
  - Ensure Git is initialized and remote origin exists.
  - Ensure GitHub CLI is installed and authenticated: `gh auth status`.
  2. Branch & commit:
  - Create/switch: `git checkout -b <branch>`.
  - Stage: `git add -A`.
  - Commit: `git commit -m "<title>"` (add a second -m with a short summary if useful).
  - Push: `git push -u origin <branch>`.
  3. Prepare body:

    - Always build the PR body from `.github/pull_request_template.md` and FILL IT IN-PLACE. Do not append a second provenance block.
    - Steps:
      - Copy the template to a temp file.
      - Replace placeholders with real values (or N/A where allowed) directly in the existing sections:
        - AI Provenance: Prompt, Model, Date (UTC ISO-8601 Z), Author, Role (provider|deployer)
        - Compliance checklist: set required checkboxes, risk classification, personal data, ADM, agent mode, vendor GPAI (if deployer), attribution
        - Change-type specifics: add only relevant lines; remove optional placeholders that don’t apply
        - Tests & Risk: add rollback plan, smoke link if high-risk
      - Ensure only one `- Date:` line exists and it contains a real timestamp; remove inline hints if needed.
    - Pre-flight validation (local):
      - Confirm these patterns exist in the body:
        - Prompt:, Model:, Date: 20..-..-..T..:..:..Z, Author:
        - Role: (provider|deployer)
        - [x] No secrets/PII, [x] Agent logging enabled, [x] Kill-switch / feature flag present, [x] No prohibited practices
        - Risk classification: (limited|high), Personal data: (yes|no), Automated decision-making: (yes|no), Agent mode used: (yes|no)
        - If Role=deployer → Vendor GPAI compliance reviewed: (https://…|N/A)
      - No `<…>` or `${…}` placeholders remain.
    - Optionally append `body_append` at the end for extra notes (avoid duplicating provenance).

  4. Create PR:
  - Detect base branch (prefer repo default); fallback to `main`.
  - Run: `gh pr create -B <base> -H <branch> --title "<title>" --body-file <temp_body_path> --draft`
  - Add labels inline: `--label ai --label governance` (plus any provided).
  - Add reviewers if provided: `--reviewer user1 --reviewer user2`.
  5. Output PR URL and short summary of changes.

     Notes:

  - Language detection heuristic: use `git ls-files` to check for common extensions (e.g., `*.py`, `*.js`, `*.ts`, `*.go`, `*.java`) and toggle inputs accordingly.

    - When you introduce new language toggles locally, propose them upstream (same repo) so future pilots get them by default.

  - Labels: ensure default labels exist or create them if you have permissions; otherwise proceed without labels.

6. record_update

- Goal: Update header timestamps when this context or .github sync changes.
- Update @last_context_update after content changes.
- Update @last_github_sync after workflow syncs. Keep ISO-8601.

7. suggest_context_note

- Goal: While working, when relevant information emerges that would help future work, propose a small addition to this context.
- Constraints: Only suggest after the user asks to "read context". Keep notes concise and reusable.

8. toggle_suggestions

- Goal: Respect per-user opt-out for suggestions.
- Mechanism:

  - Local file: .ai/context.local.json (create/update).
  - Example:

    ```json

    {
      "suggestions_enabled": false,
      "user": { "name": "<name>", "email": "<email>" }
    }

    ```

  - When false, do not surface proactive suggestions; act only on explicit commands.

## What lives in .github (discover dynamically)

Always enumerate live contents from upstream first. As of this template’s creation, the upstream project contains:

- CODEOWNERS
- pull_request_template.md
- workflows/ai-governance.yml (reusable governance)
- workflows/ai-agent.yml (ChatOps helpers)
- workflows/code-review-agent.yml (code review agent)
- workflows/copilot-pr-review.yml (on-demand AI review)
- workflows/gov-review.yml (governance artifacts reviewer)
- workflows/governance-smoke.yml (smoke checks)
- workflows/pr-autolinks.yml (auto links/NAs)
- workflows/pr-governance.yml (PR governance helpers)
- workflows/run-unit-tests.yml (unit test runner)
- workflows/workflow-lint.yml (lint GitHub workflows)

If any expected file is absent upstream when bootstrapping, warn and proceed with available items only.

## Runtime profile — VS Code GitHub Copilot Agent (gpt-5)

- Environment: VS Code with GitHub Copilot Agents; model target: gpt-5.
- Style: short, skimmable outputs; minimal Markdown.
- Action cadence: only after “read context”; list intended edits first; checkpoint after a few actions or >3 file edits.
- Smallest viable change: preserve style; avoid broad refactors; split risky work.
- Terminal usage: run git/gh and quick checks in the integrated terminal; never expose secrets.
- PR hygiene: always include Provenance (Prompt/Model/Date/Author); labels ai/governance; default to draft PRs.
- Quality gates: when code/workflows change, run a fast lint/test and report PASS/FAIL succinctly.
- Suggestions policy: suggest updates only after “read context”; users can opt out via `.ai/context.local.json`.

### Context recall protocol

- Re-read before you write: before each multi-step action or tool batch, re-open this `ai-context.md` and re-read the exact sections that govern the task (typically: “Developer prompts”, “Runtime profile”, “PR body generation rules”, and “Baseline controls”), plus any referenced docs under `governance/`.
- Don’t trust memory for strict fields: when populating provenance/risk sections or workflow inputs, copy the exact scaffold and rules from this file instead of paraphrasing.
- Keep an active checklist: derive a short requirements checklist from the user’s ask and keep it visible; verify each item before ending a turn.
- Maintain a scratchpad of snippets: keep a small, ephemeral list of the exact lines you’re following (with heading names). Refresh it after every 3–5 tool calls or after editing more than ~3 files.
- Periodic refresh: if the session runs long (>10 minutes) or after significant context edits, quickly re-scan this file (search for “PR body generation rules”, “Baseline controls”, “MUST|required|ISO-8601”) to avoid missing details.
- Resolve drift immediately: if you change this context in the same PR, re-read the modified sections and reconcile instructions before continuing.
- Token discipline: when constrained, fetch only the specific snippets you need (by heading) rather than relying on summaries.

## How to: bootstrap, verify, iterate

- Bootstrap adds:
  - Reusable workflows under `.github/workflows/` (including `ai-governance.yml`, `pr-governance.yml`, `governance-smoke.yml`, `run-unit-tests.yml`).
  - PR template `.github/pull_request_template.md` aligned with governance checks.
  - A minimal `tests/governance/smoke.test.js` so smoke passes out-of-the-box.
- After opening a PR, comment `/gov` for a governance summary and Copilot review. Use `/gov links` to preview artifact links and `/gov autofill apply` to fill placeholders.
- If your repo lacks lint/tests, set empty `lint_command`/`test_command` in the consumer `pr-governance.yml` or disable those inputs.
- AI headers: ensure YAML workflows start with
  - `# @ai-generated: true` and `# @ai-tool: Copilot` at the very top.

### Notes from comparing ".github copy" vs current (for downstream consumers)

- pr-governance.yml uses a repository path in the `uses:` field. After you copy workflows into your repo, update it to your repo path or switch it to a local reference. Example:
  - uses: <OWNER>/<REPO>/.github/workflows/ai-governance.yml@main
  - or when the workflow is local: uses: ./.github/workflows/ai-governance.yml
- ScanCode job is simplified to pip install of scancode-toolkit and always uploads `scancode.json` as `scancode-report`. Ensure artifacts exist for `/gov` commands to work.
- Unit tests runner is language-aware and skips when files aren’t present. If your project has nested apps (e.g., UI/backend folders), adapt the install-and-test blocks accordingly.
- Governance smoke workflow skips when `tests/governance/smoke.test.js` is absent. Keep or remove this file based on your project layout.
- ChatOps commands (`/gov`, `/gov links`, `/gov autofill apply`, `/gov copilot`) assume the workflow names:
  - "PR Governance (licenses & secrets)" and "Run Unit Tests". If you rename workflows, update the matching logic in `pr-autolinks.yml` and `gov-review.yml`.
- CODEOWNERS in the template points to placeholders (e.g., @ErykKul). Replace with your org teams (e.g., @libis/security) to enforce reviews on governance and policy paths.
- Lint/Test inputs: the consumer `pr-governance.yml` accepts `lint_command` and `test_command`. Set them to project-specific commands (e.g., `make fmt && make check` and `make test`), or set to empty strings `''` to skip when your repo has no lint/tests yet.

## PR body generation rules (for Copilot/Agents)

Do not fabricate links or closing keywords:

- Only include “Closes #<num>”, “Fixes #<num>”, etc., when a real issue exists.
- Never add “Closes N/A” or similar placeholders.
- If there’s no issue, omit the closing keyword entirely.

Global body constraints (must follow for this repo):

- No backticks anywhere in the PR body. Do not format text with inline/backtick code blocks. Prefer plain text or single quotes if needed.
- No unresolved placeholders: do not leave `<...>`, `${...}`, `$(...)`, or similar tokens anywhere in the body.
- When referencing code identifiers, write them as plain text without backticks.
- If you copy a template snippet, scrub it to remove any of the above before posting.

Summary (must-fill):

- Provide 1–3 bullets summarizing what changed and why.
- Link related issues when applicable.
- Do not insert a placeholder bullet if there’s nothing substantive to add; keep the section concise.

Golden scaffold (fill exactly; replace all <…> with a concrete value or N/A):

- Prompt: <paste exact prompt or link to prompt file/snippet>
- Model: <e.g., GitHub Copilot gpt-5>
- Date: <UTC ISO-8601, e.g., 2025-09-12T14:23:45Z>
- Author: @<github-handle>  // Do NOT include names or emails
- Role: provider|deployer (choose one)
- Vendor GPAI compliance reviewed: <https://… or N/A> (required if Role=deployer)
- [ ] No secrets/PII
- Risk classification: limited|high
- Personal data: yes|no
- DPIA: <https://… or N/A>
- Automated decision-making: yes|no
- Agent mode used: yes|no
- [ ] Agent logging enabled
- [ ] Kill-switch / feature flag present
- [ ] No prohibited practices under EU AI Act
- Attribution: <https://… or N/A>

Strict fill rules for the model:

- Author field MUST be a GitHub handle only (e.g., @octocat). Names and emails are forbidden.
- No backticks anywhere in the PR body (repo policy; enforced by CI/local checker).
- No unresolved placeholders anywhere in the PR body: remove/replace `<…>`, `${…}`, `$(…)`.

Strict fill rules for the model:

- Author field MUST be a GitHub handle only (e.g., @octocat). Names and emails are forbidden.

Self-check before posting (the agent should verify these patterns):

- Date matches: ^20\d{2}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$ and does not contain ${ or <>.
- Contains: Prompt:, Model:, Date:, Author:.
- Contains: Role: (provider|deployer) exactly one.
- Contains: Risk classification: (limited|high).
- Contains: Personal data: (yes|no), Automated decision-making: (yes|no), Agent mode used: (yes|no).
- Contains checkbox: [x] No secrets/PII (case-insensitive).
- If UI changed, includes transparency and accessibility lines; if media changed, includes AI content labeled + C2PA; if backend changed, includes ASVS.
- If risk=high or ADM=yes, includes Oversight plan link, Rollback plan, Smoke test link.

Local checker hard constraints (what a local script would enforce):

- Script (optional): `scripts/local_provenance_check.sh <owner/repo> <pr-number>` pulls the current PR body and validates it.
- It fails if the body contains any backticks, `${...}`, `$(...)`, or `<...>` placeholders anywhere.
- Required checkboxes/fields include: `[x] No secrets/PII`, `[x] Agent logging enabled`, `[x] Kill-switch / feature flag present`, Role, Risk classification, Personal data, Automated decision-making, Agent mode used.
- When Agent mode is yes or Risk is high, it also requires `[x] Human oversight retained`.
- Before creating/editing a PR, scrub the body to remove disallowed tokens and ensure all required lines are present.

Two minimal examples

Note: Examples must avoid PII; use `@<github-handle>` only for attribution.

- Limited, non-UI backend change
  - Prompt: “Refactor YAML linter invocation to use pinned version; add smoke test.”
  - Model: GitHub Copilot gpt-5
  - Date: 2025-09-12T10:21:36Z
  - Author: @github-handle
  - Role: deployer
  - [x] No secrets/PII
  - Risk classification: limited
  - Personal data: no
  - DPIA: N/A
  - Automated decision-making: no
  - Agent mode used: yes
  - [x] Agent logging enabled
  - [x] Kill-switch / feature flag present
  - [x] No prohibited practices under EU AI Act
  - Backend/API changed:

    - ASVS: N/A

  - Attribution: N/A

- High risk, UI changed, ADM yes
  - Prompt: “Add user-facing agent output; update transparency; link oversight and smoke.”
  - Model: GitHub Copilot gpt-5
  - Date: 2025-09-12T10:45:03Z
  - Author: @github-handle
  - Role: deployer
  - [x] No secrets/PII
  - Risk classification: high
  - Personal data: yes
  - DPIA: https://example.org/dpia/ai-feature
  - Automated decision-making: yes
  - Agent mode used: yes
  - [x] Agent logging enabled
  - [x] Kill-switch / feature flag present
  - [x] No prohibited practices under EU AI Act
  - UI changed:

    - [x] Transparency notice updated
    - Accessibility statement: https://example.org/accessibility

  - Media assets changed:

    - [x] AI content labeled
    - C2PA: N/A

  - Backend/API changed:

    - ASVS: https://example.org/asvs/review

  - Oversight plan: https://example.org/oversight/plan
  - Rollback plan: Feature flag off; revert PR.
  - Smoke test: https://github.com/libis/your-repo/actions/runs/123456789
  - Attribution: N/A

## Baseline controls to carry into all repos

- Provenance in every PR: prompt/model/date/author (@handle only) + reviewer checks (no secrets/PII, licensing OK).
- No PII policy: do not include personal names, email addresses, phone numbers, or other identifiers in PR bodies, commit messages, or logs. Use @<github-handle> only where author attribution is required.
- License/IP hygiene: ScanCode in CI blocks AGPL/GPL/LGPL; use dependency review; avoid unapproved code pastes.
- Transparency (EU AI Act Art. 50): label AI-generated summaries; include disclosure text for user-facing outputs.
- Avoid prohibited practices (Art. 5): no emotion inference in workplace/education, no social scoring, no manipulative techniques, no biometric categorization.
- Annex III guardrails: exclude high-risk domains unless escalated.
- DPIA readiness: for user-facing agents; no PII in prompts/repos.
- Monitoring + rollback: SLIs (success %, defect %, unsafe block %, p95 latency) and feature-flag rollback.
- Pause rule: if validated error rate > 2% or any license/privacy/safety incident, pause and root-cause.

## Agent coding guidelines (enforced by this context)

- Prefer the smallest viable change

  - Keep diffs minimal; preserve existing style and public APIs.
  - Reuse existing utilities; avoid duplication and broad refactors.
  - Defer opportunistic cleanups to a separate PR.

- Commit and PR discipline

  - Small, focused commits; one concern per commit.
  - Commit message: `type(scope): summary` with a brief rationale and risk notes.
  - Aim for compact PRs (< ~300 changed LOC when possible). Split larger ones.

- Safety and verification first

  - Run quick quality gates on every substantive change: Build, Lint/Typecheck, Unit tests; report PASS/FAIL in the PR.
  - Add or update minimal tests for new behavior (happy path + 1 edge case).
  - Use feature flags for risky paths; ensure clear rollback.

- Dependencies policy

  - Prefer stdlib and existing deps. Add new deps only with clear value.
  - Pin versions and update manifests/lockfiles. Check license compatibility (no AGPL/GPL/LGPL where blocked).

- Config and workflows

  - Reuse org workflows; don’t add bespoke CI beyond the minimal consumer workflow.
  - Keep workflow permissions least-privilege.

- Documentation and provenance

  - Update README or inline docs when behavior or interfaces change.
  - Use `log_provenance` to append AI-Assistance details to the PR body.

- Security, privacy, and IP

  - Never include secrets/PII; scrub logs; avoid leaking tokens.
  - Respect copyright and licensing; cite sources where applicable.

- Handling ambiguity

  - If under-specified, state 1–2 explicit assumptions and proceed; invite correction.
  - If blocked by constraints, propose a minimal alternative and stop for confirmation.

- Non-functional checks

  - Keep accessibility in mind for user-facing outputs.
  - Note performance characteristics; avoid clear regressions; document complexity changes.

- PR automation

  - Use `open_pr` to branch, commit, push, and create a draft PR via GitHub CLI with labels and reviewers.

- Suggestions policy
  - Only suggest context updates after the user invokes “read context”; honor user opt-out via `.ai/context.local.json`.

## Developer prompts (after “read context”)

- bootstrap → inspect upstream .github, propose copy + minimal consumer workflow if missing, then PR.
- verify → report context/.github drift; propose update_workflows if needed.
- update_workflows → sync .github with diffs and PR.
- log_provenance → add the provenance block to the PR body if missing.
- open_pr → branch, commit, push, and create a PR via GitHub CLI using the repo template.
- record_update → refresh timestamps in header.
- suggest_context_note → propose adding a concise, reusable note.
- toggle_suggestions off → write .ai/context.local.json to disable suggestions.

## Source references (for reuse)

- Project (source of truth): https://github.com/libis/ai-transition
- Pilot starter README (consumer workflow example): https://github.com/libis/ai-transition/blob/main/templates/pilot-starter/README.md
- Governance checks: https://github.com/libis/ai-transition/blob/main/governance/ci_checklist.md
- Risk mitigation matrix: https://github.com/libis/ai-transition/blob/main/governance/risk_mitigation_matrix.md
- EU AI Act notes: https://github.com/libis/ai-transition/blob/main/EU_AI_Act_gh_copilot.md
- Agent deployment controls: https://github.com/libis/ai-transition/blob/main/ai_agents_deployment.md
- Compliance table: https://github.com/libis/ai-transition/blob/main/LIBIS_AI_Agent_Compliance_Table.md

## State (maintained by agent)

```json

{
  "template_source": "https://github.com/libis/ai-transition/blob/main/templates/pilot-starter/ai-context.md",
  "template_version": "0.6.0",
  "last_context_update": "2025-09-16T00:00:00Z",
  "last_github_sync": "2025-09-16T00:00:00Z",
  "last_verified_at": "2025-09-16T00:00:00Z",
  "upstream_ref": "main",
  "upstream_commit": "unknown"
}

```

Notes for maintainers

- Prefer pinning reusable workflows by tag or commit SHA instead of @main for regulated repos.
- Keep this file concise and org-agnostic; link deep policy detail from the org repo.
- If suggestions are noisy, default user-local suggestions to false via .ai/context.local.json.

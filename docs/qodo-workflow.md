# Qodo workflow in Ghostline

Ghostline uses Qodo Review as source-backed review feedback and the Qodo
Agentic Toolbox as coding-agent assistance. Neither replaces tests, GitHub's
revision history, Nox security findings, or Warden's local developer gates.

## Pull request review

The root `.pr_agent.toml` asks Qodo to publish a PR summary and code review on
published or updated pull requests. The summary includes a review-effort label.
Review comments include the available context and prior-round outcomes so a
repair loop is easier to inspect. The configuration leaves the selected
Precision preset's finding and display choices in control; it does not enable
automatic code changes or merges.

Link the issue or specification and its acceptance criteria in each PR. Qodo's
requirement and design checks need relevant source context to compare against;
Ghostline should not claim those checks ran unless Qodo's review shows them.

In GitHub, use `@qodo` in a PR comment to ask for an explanation or follow-up.
Treat the response as review discussion. Verify any proposed change against the
current diff and relevant checks before accepting it.

## Coding-agent workflow

After signing in and installing the Qodo Agentic Toolbox for Codex, use its
managed workflows at the relevant stages:

1. **Codebase Wisdom** before a change to understand dependencies, impact, and
   recent history.
2. **Get Rules** before implementation to retrieve the applicable Qodo Review
   Standards.
3. **Review** on local changes before opening the PR.
4. **Review Resolver** after Qodo posts PR findings. Scope the findings to the
   requested change, then report each as fixed, skipped with a reason, or
   reported without changes.
5. **Manage Standards** when project rules need to be created, corrected,
   retired, or re-scoped. Keep `AGENTS.md` as the repository-owned source and
   verify the imported rules are active in Qodo Review Standards.

Run Ghostline's tests, build, Warden gates, and Nox scan independently. Qodo
review feedback is not a correctness guarantee, and security findings remain
owned by Nox.

## Features to evaluate in the Qodo workspace

The repository config cannot activate organization or workspace settings.
Review these in the Qodo portal after connecting the repository:

- Confirm `AGENTS.md` rules are imported and active in Review Standards.
- Keep reviews on published PRs and enable review on every push.
- Keep chat with Qodo enabled.
- Evaluate the Recall finding types (skill insights, specification gaps, and
  design gaps) while keeping automatic remediation disabled. Ghostline's
  current Precision preset intentionally prioritizes fewer, actionable
  comments, so change this only as an explicit workspace choice.
- Connect related repositories before relying on cross-repository findings.
- Try latest-dependency review on a dependency-change PR and inspect the
  revision and evidence Qodo reports.
- Treat PR Triage, analytics, and cross-repository Software Map as team-scale
  capabilities to evaluate when the repository set and review volume justify
  them; confirm plan and access requirements in the workspace.

In Ghostline, imported Qodo comments retain their source URL and inspected
revision when Qodo marks one. Their sequence does not prove a repair caused a
finding to disappear.

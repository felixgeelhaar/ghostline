# Ghostline — Product Intent

**Status:** Working product intent
**Context:** Qodo Build a Software Factory in 30 Days Hackathon
**Date:** 2026-11-03
**Working name:** Ghostline
**Tagline:** See the work. Trace the decisions. Take control.

> This document captures product intent and agreed boundaries, not a
> finalized PRD, architecture, or commitment to implement every future
> capability.

## Status note (2026-11-21)

This note was added when the document was first committed. The intent below is unchanged from the 2026-11-03 version, except that tables flattened when the text was pasted were rebuilt. Read it as the Day 1 record: where it describes the next step or an open question, this note is the current state.

- **Superseded sections:** §6 and §17 (Day 1 scope and next action) are done. Of the §16 open decisions:
  - 2 is settled: this repository exists.
  - 3 is settled: Qodo findings are read from published PR comments, and through Qodo's MCP when configured.
  - 4 is settled: Day 1 imported Ghostline's own PR.
  - 5 is partly settled: [hosted-deployment.md](hosted-deployment.md) describes a single-container deployment.
  - 1, 6, 7 and 8 remain open.

- **Delivered:**
  - Stage A: PR import, commit timeline, revision focus.
  - Stage B: source-linked Qodo comments with revision coverage.
  - Parts of Stage C: recurring findings and **Deep Trace** (`frontend/src/presentation/DeepTrace.vue`).
  - Revision-bound GitHub checks and annotations, and a trust-checked **Nox** report (see the README and [threat model](threat-model.md)).
  - The working palette below is the UI's palette.
- **Netrunner:** GitHub and Qodo translation both exist now (the §10 plan said to add Qodo later), but not yet as a separate package. Tool translation currently lives in edge files under `backend/`: `github.go`, `reviews.go` and `qodo_mcp.go` (Qodo), and `nox_report.go`. The domain rules sit in `backend/internal/domain`. Extracting these adapters into a Netrunner package is the obvious step once a fourth source arrives.
- **Not started:** Netspace, Override, Forensics as a named view, SQLite persistence, and the review-and-repair supervision loop.
- **Direction since Day 14:** the hackathon showed that the most useful single answer is whether a PR is ready to merge, based only on evidence about its exact current head. It also showed that stale review coverage, renamed required checks and labels that flicker are common. That serves hypothesis 3 below ("Revision-linked evidence improves decisions") and the promise "trust only the evidence that actually applies." The issues are [#91](https://github.com/felixgeelhaar/ghostline/issues/91) (readiness verdict), [#92](https://github.com/felixgeelhaar/ghostline/issues/92) (README), [#93](https://github.com/felixgeelhaar/ghostline/issues/93) (read-only API for agents) and [#94](https://github.com/felixgeelhaar/ghostline/issues/94) (open-PR list). The readiness verdict is Netrunner's first cross-tool output: GitHub checks, Qodo reviews and Nox reports, joined only on the commit they cover.

---

## 1. Executive intent

Ghostline is a cyberpunk-inspired engineering control room for AI-assisted software development. It helps engineers observe how changes evolve through coding-agent work, code review, repair, and verification; understand why automation becomes blocked; and intervene with useful context.

The initial experience is **change-centric, not session-centric**. A pull request or engineering change is the anchor around which commits, review findings, agent actions, validations, and human decisions can eventually be understood.

Ghostline should make a software factory easier to operate, not merely prettier to watch. Its visual language is distinctive—cinematic cyberpunk instrumentation, angular HUD panels, neon signals, and an eventual abstract WebGL topology—but the underlying information must remain accurate, actionable, and understandable.

The immediate goal is to deliver an excellent Qodo-centered hackathon project, incrementally, without prematurely constructing a generalized agent orchestration or observability platform.

## 2. Why this product

Coding agents can implement changes quickly, but engineers still need to know:

- What work is progressing, waiting, or blocked?
- What did Qodo review find, and which revision did it inspect?
- Did a repair actually address a finding, or did the same issue recur?
- Which actions and evidence explain the current state of a change?
- What requires a human decision, and how can the engineer return to their existing coding environment?
- Which checks ran against the current revision rather than an earlier one?

Today, answering these questions can require moving between agent transcripts, GitHub, code-review results, and validation tools.

**Core opportunity:** Turn fragmented review-and-repair activity into an understandable, actionable change history.

**Caution:** The need for an entirely separate product, the value of 3D presentation, and the reliability of cross-tool causal reconstruction remain hypotheses. The hackathon is an opportunity to test them.

## 3. Product promise

For engineers using AI coding agents, Ghostline is a control room that reveals how a software change progresses through review and repair, highlights when automation needs attention, and provides evidence for the next engineering decision.

Three promises:

1. **See the work:** Make actual workflow events and revision changes visible.
2. **Trace the decisions:** Link review feedback, subsequent edits, and verification to their sources, without inventing causality.
3. **Take control:** Provide clear intervention context while allowing engineers to keep using their preferred IDE and coding agent.

Ghostline is not intended to replace Cursor, Claude Code, Codex, GitHub, Qodo, or existing CI.

## 4. Target users and jobs-to-be-done

### Primary user

An individual software engineer who delegates implementation to coding agents and remains responsible for reviewing, correcting, and shipping the result.

### Secondary users (future)

Tech leads supervising multiple concurrent changes, and platform/engineering-enablement teams evaluating autonomous development workflows.

### Priority jobs

| Job | Trigger | Desired outcome |
|---|---|---|
| Understand a change | A PR or agent-generated patch exists | Quickly see what changed and when |
| Interpret review feedback | Qodo produces findings | Understand actionable issues and affected revision |
| Diagnose a repair loop | The same concern persists across attempts | Find where progress stopped without rereading every transcript |
| Decide whether to intervene | Automation stalls or needs guidance | Provide a useful next action with minimal context switching |
| Assess verification completion | An agent reports completion | See which checks apply to the current revision |
| Reconstruct a history | A change is questioned later | Inspect the relevant source-backed sequence |

### Explicit user-experience constraint

Engineers should be able to start work in their existing coding environment. Ghostline is a companion and control room, not a mandatory replacement workspace. Any future pause/resume/takeover control must distinguish observed external sessions from sessions Ghostline actually manages.

## 5. Hackathon objective

**Primary objective:** Maximize the chance of a strong Qodo Software Factory Hackathon submission through real, focused, verifiable increments.

The project should make Qodo central to its software-factory story:

- A coding agent implements a change.
- Qodo reviews that change.
- Review findings become understandable feedback.
- A coding agent may attempt a repair.
- Ghostline shows change evolution and recurring feedback.
- A human can understand and guide a stalled workflow.
- A subsequent review and relevant checks provide evidence about the result.

The project should not become a general-purpose verification engine or a broad agent orchestration platform merely to appear ambitious.

### Signature demonstration (aspirational)

A single change—such as an idempotency improvement to a payment service—moves through implementation, Qodo review, repair attempts, human guidance, and re-review. Ghostline reveals a repeated review concern and makes the intervention meaningful. The abstract signal field reflects real recorded events. A prepared replay is acceptable for presentation, but must be clearly distinguished from live execution.

This is a target demo narrative, not a claim that Qodo will necessarily produce a specific finding.

## 6. Day 1: first delivery

### Scope

Import a public GitHub pull request and display its commit timeline.

The user enters a GitHub PR URL. Ghostline fetches PR metadata and commits, then presents a readable, cyberpunk-styled timeline.

### Acceptance criteria

- Accept and validate a GitHub pull-request URL.
- Retrieve real public PR metadata and commits.
- Show repository, PR number, title, state, and chronological commits.
- Provide useful loading, invalid-input, not-found, and API-error states.
- Clearly label commit timestamps; do not pretend they represent push or complete PR-event chronology.
- Run the application and demonstrate the capability.
- Open a focused implementation PR and run a Qodo review.
- Record at least one observation about the review's value, limitations, or an engineering decision it prompted.
- Publish the required hackathon introduction and Day 1 delivery evidence.

### Scope exclusions

No database required, no generic adapter SDK, no 3D visualization requirement, no autonomous repair loop, no OAuth requirement, and no mandatory Nox/Warden/Relicta integration.

### Elegant demo opportunity

Use Ghostline to display its own first implementation PR.

## 7. Progressive product capabilities

### Stage A — Change visibility

- Import and inspect GitHub PRs.
- Show commits and revision evolution.
- Establish a consistent change identity.

### Stage B — Qodo review feedback

- Attach actual Qodo review outcomes to the relevant change/revision, subject to available integration interfaces.
- Show findings and their original source.
- Distinguish review outcomes from independently verified correctness.
- Explore review-to-repair relationships.

### Stage C — Review-and-repair supervision

- Display repeated attempts and recurring findings.
- Show a focused trace of the feedback loop.
- Surface a concise engineer-facing intervention summary.
- Support guidance through a managed workflow only where technically possible.

### Stage D — Netspace and integrations

- Introduce a live/replayable abstract topology driven by actual events.
- Add optional Warden and Nox signals where they strengthen the Qodo-centered workflow.
- Improve correlation across revisions and tools.
- Keep non-3D views fully functional.

These stages are directional, not fixed hackathon deadlines.

## 8. Key interaction concepts

### Change timeline

The default representation of a software change, initially based on PR commits and later enriched with review and validation events.

### Living review loop

A visible progression from implementation to Qodo feedback, repair, and re-review. The UI should make it clear which findings recur and which revisions are involved.

### Deep Trace

An investigative view that isolates the sequence relevant to a finding or blocked change. Replay reconstructs recorded history; it does not fabricate an agent's hidden reasoning.

### Override (future)

A human intervention experience that provides context and a path back to the existing IDE. Exclusive takeover, pausing, or resuming is only promised for workflows with enforceable execution control.

### Netspace

An abstract WebGL/2.5D signal environment. Nodes represent agents, tools, or stages; paths represent supported relationships; pulses represent real events. A standard timeline remains available for accessibility and precise reading.

## 9. Evidence and truthfulness

Ghostline must separate:

- **Observed:** A source explicitly recorded an event or relationship.
- **Derived/correlated:** A deterministic join or rule supports a relationship, such as matching commit SHAs.
- **Inferred:** A plausible interpretation requiring corroboration.

A sequence in time is not proof of causation. A finding disappearing in a later review is not, by itself, proof that a defect was fixed. A green test result on an earlier commit does not validate a later revision.

Each integrated event should preserve source identity, native identifiers, relevant revision references, timestamps, and a link/reference to original evidence where available.

**Design rule:** Every glow represents activity. Every pulse represents an event. Every connection represents a relationship whose evidentiary status can be inspected.

## 10. Product boundaries and ecosystem

| System | Ownership | Ghostline relationship |
|---|---|---|
| Qodo | Semantic review, standards, review feedback | Central hackathon integration |
| Coding agents | Code generation and edits | Observe and, where supported, guide |
| GitHub | Repositories, PRs, commits, checks | Initial change source |
| Warden | Git validation, gates, provenance | Optional evidence source |
| Nox | Security scanning and authorized validation | Optional security signal source |
| Relicta | Release governance and authoritative release decisions | Independent product; conceptual inspiration only |
| Ghostline | Change visibility, review-loop diagnosis, interaction and presentation | Does not duplicate source tools' authority |

Ghostline remains separate from Relicta. It may borrow concepts such as explicit state transitions, event histories, provenance, evidence, and governance boundaries without reproducing Relicta's release governance.

### Netrunner

Netrunner is the working name for Ghostline's anti-corruption/translation boundary. It can adapt tool-specific events into a small common vocabulary while preserving native semantics and evidence.

For the hackathon it should begin as an internal Go package, not a separate service or generalized plugin platform. Start with GitHub; add Qodo when the actual interface is established.

## 11. Visual and brand intent

### Working identity

| Concept | Name |
|---|---|
| Product | Ghostline |
| Integration concept | Netrunner |
| Spatial experience | Netspace |
| Investigation | Trace / Deep Trace |
| Human intervention | Override |
| Evidence inspection | Forensics |

Names are provisional pending availability checks.

### Visual direction

A cinematic cyberpunk netrunner's workstation that behaves like a serious engineering instrument.

Retain the atmosphere of the original dashboard exploration: neon cyan and magenta, angular HUD geometry, holographic depth, futuristic typography, dark translucent surfaces, and a sense of operating within a computational world.

Experiment with abstract data environments in place of some cityscape/character imagery; do not strip away the atmosphere that made the original concept compelling.

### Working palette

| Token | Color | Role |
|---|---|---|
| Void | `#090B16` | Deep background |
| Neon cyan | `#00E5FF` | Active signals and focus |
| Magenta | `#FF2BD6` | Review/trace accent |
| Yellow | `#FCEE0A` | Attention/warnings |
| Green | `#00FFA3` | Successful verified states |

Colors are a starting point, not a final accessibility specification. Status must never depend on color alone.

### Visual principles

- Angular, asymmetric panels and precise instrumentation.
- Restrained glow on meaningful activity.
- Legible body typography and monospace technical metadata.
- Subtle signal pulses and scan effects, with reduced-motion support.
- Cinematic atmosphere without compromising code-review readability.
- A custom Ghostline design system rather than a pre-styled UI kit.
- Accessible headless primitives for complex controls when useful.

The original generated cinematic dashboard is the primary conceptual visual reference; no final UI assets are committed.

## 12. Selected technology direction

| Area | Choice |
|---|---|
| Backend | Go |
| HTTP | Gin |
| SQL access | sqlc |
| Database when needed | SQLite |
| Logging | Bolt (user-selected logging library; API to verify) |
| State machines | statekit |
| Security | Fortify (exact integration to verify) |
| GitHub client | go-github |
| Frontend | Vue 3 + TypeScript + Vite |
| Styling | Tailwind CSS v4 and custom design tokens |
| Accessible primitives | Reka UI where useful |
| Spatial visualization | Three.js + TresJS |
| Testing | Go tests, Vitest, Playwright |

**Architecture bias:** A small Go backend and Vue frontend, initially simple to run and demonstrate. Avoid microservices, Kafka, a graph database, and generalized workflow infrastructure without concrete need.

The presence of statekit, Fortify, and sqlc in the selected stack does not mean they must be artificially used in Day 1.

## 13. Explicit non-goals

For the hackathon, Ghostline is not:

- A replacement for Cursor, Claude Code, or Codex.
- A new Qodo competitor or semantic review engine.
- A generalized autonomous software factory platform.
- A new adversarial testing, exploit generation, or security-scanning engine.
- A replacement for Warden's validation or Relicta's release governance.
- A generic LLM tracing or token-cost dashboard.
- A 3D visualization disconnected from real engineering events.
- A custom component-library project or a distributed infrastructure showcase.

## 14. Research-informed hypotheses

Prior competitive and research exploration suggests that generic agent dashboards, session replay, and spatial visualization already have alternatives. The potential differentiation lies in Qodo-centered change evolution and actionable review-loop diagnosis.

| Hypothesis | Validation approach |
|---|---|
| Engineers understand review/repair progress faster with Ghostline | Compare against GitHub + agent transcript + Qodo |
| Repeated findings can be surfaced reliably | Use recorded review/repair runs with known ground truth |
| Revision-linked evidence improves decisions | Test scenarios with current, stale, and missing checks |
| Netspace improves comprehension | Compare against a conventional timeline/2D graph |
| The product avoids unnecessary context switching | Observe real workflow usage and intervention steps |

These are hypotheses, not proven outcomes. The hackathon should prioritize working software and honest demonstrations over unsupported scientific claims.

## 15. Success criteria

### Day 1

A real PR can be imported and inspected in Ghostline; the implementation is agent-assisted, functional, reviewed by Qodo, and shared with a concrete review lesson.

### Hackathon

- A coherent sequence of small, working deliveries.
- Qodo is a meaningful, central part of the review-and-repair workflow.
- At least one genuine change evolution is visible across reviews and repairs.
- The engineer can understand why an attempted repair did or did not progress.
- Any human intervention is grounded in actual supported controls.
- The visual experience is distinctive, but backed by inspectable source events.
- The demonstration is reproducible and honest about replay versus live execution.

### Longer-term product signal

Engineers find and resolve blocked work faster or make better-informed verification decisions than they do with their existing tools.

## 16. Open decisions

> Day 1 record. Decisions 2–4 are settled and 5 partly; see the [status note](#status-note-2026-11-21).

1. Confirm the Ghostline name's GitHub, package, domain, and trademark availability.
2. Create or identify the repository.
3. Verify Qodo review access and the exact mechanisms for retrieving review findings.
4. Choose the public PR used for the first import.
5. Decide local-first versus hosted deployment when it becomes relevant.
6. Verify Bolt, statekit, and Fortify APIs before integration.
7. Decide when Netspace adds enough value to justify implementation.
8. Determine the minimum real-world event data needed for a credible review-loop demo.

## 17. Immediate next action

> Day 1 record, done. For current next steps see the [status note](#status-note-2026-11-21) and issues #91–#94.

Start Day 1.

Build one vertical slice: GitHub PR URL → Go/Gin fetch → normalized metadata and commits → Vue timeline.

Use a coding agent, run it, open a focused PR, request Qodo review, evaluate the feedback, and post the required hackathon introduction and delivery evidence.

Everything beyond that should be justified by a later working increment.

---

## North star

> **Ghostline makes autonomous engineering understandable and controllable.**
>
> Engineers should see how changes evolve, understand the feedback that shapes them, know when automation needs help, and trust only the evidence that actually applies to the code being delivered.

Cyberpunk in atmosphere. Scientific rigor in evidence. Qodo at the center of the hackathon workflow.

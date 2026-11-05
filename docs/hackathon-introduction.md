# Ghostline — See the work. Trace the decisions. Take control.

Coding agents can produce changes quickly, while engineers still have to piece together what changed and which revision they are looking at. Ghostline is a change-focused control room for that work: it makes pull requests, review feedback, repairs, and validation easier to follow while keeping evidence tied to its source.

For Day 1, Ghostline imports a public GitHub pull request and displays its real commits in a chronological timeline. A Go/Gin backend fetches GitHub data; a Vue 3 + TypeScript interface validates the URL before import and uses Zod to validate the returned API response. Docker Compose runs the demo, and repository checks run in GitHub Actions. The timeline labels commit timestamps accurately and does not present them as push or full pull-request event history.

The first increment was deliberately small. Ghostline now imports Qodo's published review comments alongside the revisions they explicitly identify, preserves finding details and source links, and offers later PR revisions as context to inspect. Commit order is not proof of repair, and Qodo feedback remains separate from independent verification.

Repository: https://github.com/felixgeelhaar/ghostline  
Implementation PR: https://github.com/felixgeelhaar/ghostline/pull/1  
Day 1 evidence: https://github.com/felixgeelhaar/ghostline/blob/feat/day-1-nox-status/docs/day-1-evidence.md

![Ghostline importing its own PR and showing its commit timeline](https://github.com/felixgeelhaar/ghostline/blob/feat/day-1-nox-status/docs/assets/day-1-pr-timeline.png?raw=true)

**Qodo review lesson:** Qodo found an incorrect argument order in the Nox wrapper. The wrapper has since been removed and Warden calls Nox directly. A later review comment referred to that deleted wrapper, which is a useful reminder to check review feedback against the current revision.

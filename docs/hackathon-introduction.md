# Hackathon introduction draft

## Ghostline — See the work. Trace the decisions. Take control.

Coding agents can move quickly, while engineers still have to piece together what changed, which revision was reviewed, and what evidence supports shipping it. Ghostline is a cyberpunk-inspired engineering control room for that work: a change-centered view of pull requests, review feedback, repairs, and validation, with source evidence kept visible.

For Day 1, Ghostline imports a public GitHub pull request and presents its real commits in a readable timeline. A Go/Gin backend fetches GitHub data; a Vue 3 + TypeScript interface validates inputs with Zod. Docker Compose starts the app. Optional repository forensics run Nox offline and the repository's configured Warden gate, with findings kept distinct from GitHub revision evidence.

The first increment is deliberately narrow. The next step is to connect a genuine Qodo review to the change and trace its feedback across revisions, without claiming that sequence alone proves causation or correctness.

Repository: https://github.com/felixgeelhaar/ghostline

Implementation PR: https://github.com/felixgeelhaar/ghostline/pull/1

Day 1 evidence: https://github.com/felixgeelhaar/ghostline/blob/feat/day-1-nox-status/docs/day-1-evidence.md

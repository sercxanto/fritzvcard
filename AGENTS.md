# AGENTS.md

Read [CONTRIBUTING.md](CONTRIBUTING.md) first. It is the source of truth for setup, tasks,
project layout, tests and conventions.

Rules that apply to agents only:

- Never contact a FRITZ!Box or any other device on the local network.
- Never ask for or handle real credentials (passwords, tokens, session IDs).
- Work only with anonymized material: the "Probe findings" in the implementation plan and
  reviewed recordings the human shares with you. Raw probe recordings (`fritzprobe-*.txt`) stay
  local and git-ignored; do not read or commit them unless the human hands one over.
- When real-box behavior is needed, ask the human to run the probe program and share the
  reviewed, anonymized output.

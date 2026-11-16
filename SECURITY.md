# Security policy

## Project status

Evaris is in 0.x development. Only the latest 0.3.x release and the default
branch receive security fixes; there is no long-term support line. The CLI
validates experiments, prepares workspaces, imports external results, grades
them, writes reports, compares runs, and can call an external Invaris binary.

## Reporting a vulnerability

Use the repository's GitHub **Security → Report a vulnerability** option if it is
available. Private vulnerability reporting has not been verified as enabled.
If that option is unavailable, open an issue containing only a request for a
private security contact. Wait for the maintainer to establish a private channel
before sharing vulnerability details.

Do not post exploit details, credentials, private evaluation data, or sensitive
logs in a public issue. In a private report, include the affected revision,
environment, reproduction steps, expected and observed behavior, impact, and a
minimal example with secrets removed. No response-time guarantee is currently
established.

## Security boundaries

Evaris processes fixtures, imported agent outputs, and evidence that may be
untrusted. Filesystem separation is not a sandbox. Command graders and Invaris
guards run only with `--allow-commands`, are trusted by the operator, and are
not sandboxed; see [docs/security.md](docs/security.md).

Implementation reviews should address path traversal, symlink escapes, reviewer
oracle disclosure, source mutation, stale or malformed evidence, secret leakage,
artifact size limits, process cancellation, and interrupted writes. A verifier's
acceptance must not be presented as proof of every evaluation requirement.

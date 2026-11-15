# Security policy

## Project status

Evaris is pre-release. The current CLI validates experiments, prepares workspaces,
and imports external results.
There are no published release lines with a security maintenance commitment.
Security fixes during development target the default branch; a release support
policy will be documented when releases exist.

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

## Intended security boundaries

The planned framework will process fixtures, imported agent outputs, and evidence
that may be untrusted. Filesystem separation alone will not provide a sandbox.
Running user-supplied or agent-generated commands requires an appropriate
execution environment and explicit authorization.

Implementation reviews should address path traversal, symlink escapes, reviewer
oracle disclosure, source mutation, stale or malformed evidence, secret leakage,
artifact size limits, process cancellation, and interrupted writes. A verifier's
acceptance must not be presented as proof of every evaluation requirement.

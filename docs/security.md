# Local execution boundaries

Evaris does not sandbox commands. Use trusted absolute executable paths and literal
argument arrays in private oracles. --allow-commands is additionally required.
Command grading uses a separate scratch copy for every invocation, an allowlisted
environment, bounded stdout/stderr, timeouts and process-group cancellation on
Linux/macOS. Detached descendants and external side effects are not contained.

Raw imported content and command observations can contain secrets. They stay in
private evidence files; public summaries do not inline their contents or command
stderr. There is no automatic secret detection or redaction guarantee. Review
all artifacts before sharing. Hashes identify bytes; they do not authenticate hosts.

Optional Invaris verification (see [invaris.md](invaris.md)) also requires
--allow-commands and an absolute --invaris path. Its harness guard commands are
trusted, run in a scratch copy of the imported tree with only PATH inherited, and
are not sandboxed. A relative guard program can execute agent-produced files.

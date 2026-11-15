# Evidence storage implementation status

The initial internal storage primitives provide bounded regular-file manifests
and exclusive transaction directories. A transaction writes files with exclusive
creation, flushes and closes them, and creates COMMITTED last. Existing files are
never replaced. Manifests include sorted paths, file hashes, sizes and executable
status. Snapshot limits are 2,000 entries, 10 MiB per file and 100 MiB per tree.
Symlinks and special files are rejected.

Preparation and import now use committed private input archives and sealed records.
Read boundaries validate schemas, reference hashes, full manifests and run/preparation
identity. Copying uses rooted reads, bounded bytes and source revalidation. Tests
exercise second-arm and publication failures, cancellation and cleanup. The wider
T02 fault-injection matrix remains incomplete. Ordinary directory
separation is not a sandbox. Full lifecycle contracts are in the master plan.


Preparation freezes exact experiment/task/oracle/instruction bytes and independent
fixture copies under inputs/<config digest>. Private source-path metadata prevents
importing original reviewer files as output evidence. Preparation records live under
preparations/<id>; imported responses, trees and transcripts under runs/<id>.
An exclusive slot reservation prevents duplicate imports for the same prepared arm.
A crash may leave a slot reservation; inspect retained evidence before manual cleanup.
COMMITTED on preparation/run/input records contains a manifest digest; the store
root marker indicates completed initialization and is not a signature.

Reserved TASK.txt names are rejected case-insensitively to avoid collisions on
macOS filesystems. Public tasks can be fixture files under another name, and cases
can share an identical fixture root. Empty fixtures/final trees are supported.

## Evaluations and reports

`evaluate` writes evaluations/<id>/evaluation.json and private grader observations.
The read/write boundary checks the exact frozen criterion set, required flags,
source-authorization gate, run/oracle digests, evidence references and recomputed
assessment. Infrastructure errors do not imply agent quality failure. A known
required failure stays visible alongside tool errors. Semantic review, if required,
stays pending; P0 does not import human approvals.

`report` writes reports/<id>/report.json and report.md. Missing arms are explicit
INCONCLUSIVE rows. Old evaluations remain immutable and referenced. Reports never
inline artifact contents or raw command streams. Command evidence records bounded
process output, executable identity, before/after scratch manifests and cleanup
status. Hashes identify bytes; they do not authenticate tools.

# Scope each Mod Transaction to one Profile

A multi-profile operation is one Mod Transaction per affected Profile and reports a result per Profile: a failure in one neither undoes nor blocks another. Within a Profile, recovery resolves to a complete previous or intended state before Astradew launches it. A single atomic commit spanning several directories, possibly several disks, and SQLite is not a guarantee the filesystem can honour.

## Consequences

A Recovery Snapshot is mandatory until its transaction commits, so an operation refuses to start when there is no room for one. The retention setting governs only how long a Rollback Target survives after a successful commit.

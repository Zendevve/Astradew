# Resolve updates by comparing package, deployed copy, and candidate

An update compares three states: the Package that was deployed, the Deployed Copy, and the candidate Package. Unchanged upstream-owned files are replaced or removed, non-conflicting user-created files survive, the local root `config.json` is preserved with the upstream variant kept beside it when both changed, and anything else that would destroy a local modification or overwrite a user-created file pauses for an explicit choice.

## Consequences

Obsolete upstream files are not carried into the new version, and no update merges arbitrary files to keep itself automatic.

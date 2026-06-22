# Security policy

## Supported versions

cride is currently pre-1.0. Security fixes are applied to the latest code on
the default branch; older snapshots are not maintained as separate supported
release lines.

## Reporting a vulnerability

Please do not open a public issue for a vulnerability that could put users or
their repositories at risk. Use the repository's private vulnerability
reporting feature on GitHub instead. Include:

- the affected revision or release;
- the operating system and relevant tool versions;
- steps to reproduce the issue;
- the expected impact; and
- any suggested mitigation, if known.

If private vulnerability reporting is not enabled yet, contact the repository
owner privately through their published organization contact and avoid sharing
exploit details in public channels.

The maintainers will acknowledge a report, investigate it, and coordinate a
fix and disclosure timeline appropriate to the severity. Please allow time for
a patch to be prepared before publishing details.

## Security boundaries

cride operates on local source repositories and executes `git`, `rg`, and
optional language servers found on `PATH`. It does not make application-level
network requests, but those external tools process repository content and use
the current user's permissions. Review untrusted repositories and executables
with the same care you would use for other local developer tooling.

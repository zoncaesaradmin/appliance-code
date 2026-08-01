Repo-owned offline host package payload for installer-owned host capabilities.

Expected layout:

- `ubuntu/22.04/amd64/*.deb`
- `ubuntu/24.04/amd64/*.deb`

The release-input build defaults to this directory automatically. Use the
`--host-packages-dir` override only for unusual packaging workflows.

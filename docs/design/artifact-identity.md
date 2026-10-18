# Container Archive Identity

For images built by the engine, Docker's inspected ID selects the immutable
image passed to `image save`. Docker's containerd image store may use a manifest
digest for this ID; Trivy's `Metadata.ImageID` identifies the image configuration.
They are different objects and cannot be compared directly.

The engine therefore reads the single configuration named by the saved Docker
archive's `manifest.json` and derives its content digest for the Trivy comparison.
Both legacy Docker and containerd exports provide this manifest. The archive
and metadata retain their release-owned size limits, and missing, duplicate,
linked or ambiguous identity records fail before scanning. The saved image is
never selected again through a mutable tag. A Trivy configuration mismatch still
fails, including when OpenVEX is applied.

The Docker 29 regression archive in `internal/artifactsecurity/testdata` was
exported from a dependency-free scratch image with one purpose label. Its
configuration ID is
`sha256:0cf74d99b5a97c3748ef2e9f91839752b96b00d2427cb57f18919e8ed7d01ad6`;
the distinct Docker manifest ID is
`sha256:13c9308faace219384c34d1108b928face27e7e1881b1d9b0dd0ec1e84569c68`.

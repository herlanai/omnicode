# Open CodeReview

- Project: https://github.com/alibaba/open-code-review
- Version: v1.13.3 (375f64c)
- License: Apache-2.0; original LICENSE included alongside this file.
- Source: https://github.com/alibaba/open-code-review/tree/v1.13.3
- Omnicode migration: copied from the local poprsi project's pinned macOS arm64 runtime; no source modifications. SHA256SUMS records the input binary before bundle signing.
- Packaging: scripts/package-macos.sh verifies the input checksum, copies the runtime and license, then applies a local ad-hoc macOS signature.
- Runtime: delegate preview/rule only; no npm launcher, auto-update or runtime installation. OCR rules are embedded in the upstream binary. Omnicode delegates model analysis to the user's configured AI engine using an isolated, read-only project snapshot.

# Releasing SQLcery

Releases are built and published automatically by GitHub Actions when a version tag is pushed.

## Before tagging

- Make sure the working tree is clean and the release branch is ready to tag.
- Confirm `README.md`, `TASKS.md`, and any user-facing docs reflect the release.
- Ensure the repository has a GitHub remote configured, for example `origin`.
- Pick the semantic version to tag, for example `v0.1.0`.

## Local validation (optional but recommended)

- Run `mise run check-go-version`.
- Run `mise run test`.
- Run `mise run release-snapshot` and inspect the artifacts in `dist/`.

## Tag and release

- Tag HEAD: `mise run tag v0.1.0` — validates semver, creates an annotated tag locally.
- Push the tag: `git push origin v0.1.0` — triggers the GitHub Actions release workflow.
- The workflow runs tests, then GoReleaser, and publishes the release automatically.

## After publishing

- Smoke-test `go install github.com/adwinying/sqlcery/cmd/sqlcery@v0.1.0` from a clean shell.
- Confirm the release page and install instructions look correct.
- Record any follow-up packaging or automation work in `TASKS.md`.

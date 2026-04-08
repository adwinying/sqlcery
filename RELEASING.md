# Releasing SQLcery

This checklist is the initial release workflow for tagged GitHub releases built with GoReleaser.

## Before tagging

- Make sure the working tree is clean and the release branch is ready to tag.
- Confirm `README.md`, `TASKS.md`, and any user-facing docs reflect the release.
- Ensure the repository has a GitHub remote configured, for example `origin`.
- Ensure `GITHUB_TOKEN` is set with permission to create GitHub releases.
- Pick the semantic version to tag, for example `v0.1.0`.

## Local validation

- Run `mise run check-go-version`.
- Run `mise run test`.
- Run `mise run release-snapshot` and inspect the artifacts in `dist/`.

## Create the draft release

- Create an annotated tag, for example `git tag -a v0.1.0 -m "v0.1.0"`.
- Push the tag to GitHub, for example `git push origin v0.1.0`.
- Run `mise run release-draft`.
- Review the generated draft release body, then replace the placeholder highlights in `.github/release-notes.md.tmpl` if needed for the final publish.

## Publish

- Verify the draft release includes archives for macOS, Linux, and Windows plus `checksums.txt`.
- Spot-check one downloaded artifact and confirm the checksum matches.
- Publish the draft release on GitHub.

## After publishing

- Smoke-test `go install github.com/adwinying/sqlcery/cmd/sqlcery@{{TAG}}` from a clean shell.
- Confirm the release page and install instructions look correct.
- Record any follow-up packaging or automation work in `TASKS.md`.

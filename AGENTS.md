## Introduction 
We are a terminal based music player. The core concept is to be simple, minimal and easy to use. 

## Tech Stack
- All written in Go
- Written for Linux (only Linux as of now)

## Code Style Guidelines
Assume this development host has no sound card. Make the sound support generic for all Linux hosts and assume you will not be able to test the sound on this host.

HARD RULE: When the binary is run with `-v`, it MUST print the latest git tag version (e.g. `v0.1.18`), never a dev/placeholder string like `muzak321 dev`. If `VERSION` is not set at link time, fall back to `git describe --tags --always`.

## Release Workflow

When releasing a new version:

1. Update `snap/snapcraft.yaml` version field to match the target release version
2. Commit all changes (including the version bump) to main
3. Create and push a new git tag matching the version (e.g., `v0.1.20`)

The GitHub workflow (`.github/workflows/bump-homebrew-core.yml`) triggers automatically on any new tag and:
- Computes the source archive SHA256
- Opens a PR against Homebrew/homebrew-core with the updated formula

**Important:** The snapcraft version and git tag must match or the workflow will fail. Always commit the version bump in the same commit before tagging.


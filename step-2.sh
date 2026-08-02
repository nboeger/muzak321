#!/usr/bin/env bash
#
# step-2.sh — Create the GitHub Actions "STORE_LOGIN" secret
#
# This exports snapcraft store credentials and registers them with GitHub so the
# .github/workflows/snap.yml publish job can upload muzak321 to the Snap Store.
#
# Prereqs:
#   - snapcraft is installed and you can log in with your Ubuntu One account
#     (run `snapcraft login` first if you haven't).
#   - The snap name "muzak321" is registered to you (step 1).
#   - Either the `gh` CLI (recommended) or browser access to the GitHub web UI.
#
# Usage:  bash step-2.sh
#
# It writes the credentials to ./snapstore.txt, then (if `gh` is available) sets
# the STORE_LOGIN secret on the repository automatically. Otherwise it prints the
# manual path to paste the file contents into GitHub.

set -euo pipefail

SNAP_NAME="muzak321"
SECRET_NAME="STORE_LOGIN"
EXPORT_FILE="snapstore.txt"
REPO="nboeger/muzak321"

echo "==> Exporting snapcraft store credentials for '$SNAP_NAME'"
echo "    (this may open a browser or prompt for your Ubuntu One credentials)"
snapcraft export-login \
  --snaps="$SNAP_NAME" \
  --acls=package_access,package_push,package_update,package_release \
  "$EXPORT_FILE"

echo "==> Credentials written to ./$EXPORT_FILE (keep this file secret, delete it after)"

if command -v gh >/dev/null 2>&1; then
  echo "==> Registering secret '$SECRET_NAME' on $REPO via gh"
  gh secret set "$SECRET_NAME" --repo "$REPO" --body "$(cat "$EXPORT_FILE")"
  echo "==> Done. The publish job will now be able to upload to the Snap Store."
else
  echo "==> 'gh' is not installed, so the secret was NOT set automatically."
  echo "    Set it manually:"
  echo "      GitHub → Settings → Secrets and variables → Actions → New repository secret"
  echo "      Name:  $SECRET_NAME"
  echo "      Value: the full contents of ./$EXPORT_FILE"
fi

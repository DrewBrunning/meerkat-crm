#!/usr/bin/env bash
# COMPAT-02 (issue #473): assert that a device one API level BELOW the app's
# declared minSdk (26) refuses to install the debug APK outright, and does so
# with the comprehensible INSTALL_FAILED_OLDER_SDK error rather than some
# opaque failure. Proves the minSdk declaration reaches the merged manifest and
# is enforced by PackageManager -- not that Android itself works.
#
# Invoked from android-tests.yml's `android-below-min-sdk` job, inside the
# reactivecircus/android-emulator-runner `script:`. That action runs each
# physical line of an inline script in its own shell, so a multi-line inline
# script cannot share `$apk` or a `set +e` across lines -- hence a real script
# file here, where `set -e`, the APK lookup and the control flow all live in
# one process.
set -euo pipefail

apk_dir="${GITHUB_WORKSPACE:-.}/android/app/build/outputs/apk/debug"
apk="$(find "$apk_dir" -name '*.apk' | head -n1)"
if [ -z "$apk" ]; then
  echo "::error::no debug APK under $apk_dir -- did :app:assembleDebug run?"
  exit 1
fi
echo "Installing $apk on an API 24 (below minSdk 26) emulator..."

set +e
output="$(adb install "$apk" 2>&1)"
status=$?
set -e
echo "$output"

if [ "$status" -eq 0 ]; then
  echo "::error::apk installed successfully on API 24 -- minSdk 26 is no longer enforced, or this job stopped testing it"
  exit 1
fi
if ! printf '%s\n' "$output" | grep -qi 'OLDER_SDK'; then
  echo "::error::install failed as expected but not with the comprehensible INSTALL_FAILED_OLDER_SDK error: $output"
  exit 1
fi
echo "OK: install failed clearly (INSTALL_FAILED_OLDER_SDK)."

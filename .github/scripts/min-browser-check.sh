#!/usr/bin/env bash
# COMPAT-02 (issue #473): drives a real, pinned browser binary through
# WebDriver's raw HTTP protocol (curl only -- no Selenium/Playwright client
# needed) and asserts the built frontend actually renders on it. Used by
# min-version-tests.yml's browser-minimum job to prove the browserslist floor
# (docs/development/supported-runtime-matrix.md) works end-to-end, not just
# that vite.config.ts's build.target was set correctly.
#
# Usage: min-browser-check.sh <driver-port> firefox|chrome <browser-binary> <url>
#
# Assumes a WebDriver server (geckodriver for firefox, chromedriver for
# chrome) is already listening on <driver-port>.
set -euo pipefail

driver_port="$1"
kind="$2"
binary="$3"
url="$4"
base="http://127.0.0.1:${driver_port}"

case "$kind" in
  firefox)
    caps='{"capabilities":{"alwaysMatch":{"browserName":"firefox","moz:firefoxOptions":{"binary":"'"$binary"'","args":["-headless"]}}}}'
    ;;
  chrome)
    caps='{"capabilities":{"alwaysMatch":{"browserName":"chrome","goog:chromeOptions":{"binary":"'"$binary"'","args":["--headless=new","--no-sandbox","--disable-gpu"]}}}}'
    ;;
  *)
    echo "::error::min-browser-check.sh: unknown browser kind '$kind' (want firefox|chrome)"
    exit 2
    ;;
esac

# Wait for the driver server itself to accept connections before posting a
# session -- it was just started in the background by the calling step.
for _ in $(seq 1 30); do
  curl -fsS -o /dev/null "${base}/status" 2>/dev/null && break
  sleep 1
done

session_resp="$(curl -fsS -X POST "${base}/session" -H 'Content-Type: application/json' -d "$caps")"
session_id="$(echo "$session_resp" | python3 -c 'import json,sys; print(json.load(sys.stdin)["value"]["sessionId"])')"
browser_version="$(echo "$session_resp" | python3 -c 'import json,sys; print(json.load(sys.stdin)["value"]["capabilities"].get("browserVersion","?"))')"
echo "New session $session_id on $kind $browser_version"

cleanup() {
  curl -fsS -X DELETE "${base}/session/${session_id}" >/dev/null 2>&1 || true
}
trap cleanup EXIT

curl -fsS -X POST "${base}/session/${session_id}/url" -H 'Content-Type: application/json' \
  -d '{"url":"'"$url"'"}' >/dev/null

# WebDriver's POST .../url only waits for the platform page-load event
# (DOMContentLoaded/load) -- it returns as soon as the static HTML shell and
# its synchronous resources are in, well before React has mounted (i18n init,
# theme setup, an auth check all run async first). Querying title/#root the
# instant that call returns is a race, not a real "did the app load" check:
# found here querying immediately reproducibly reports the static
# <title>Mycorrhizal CRM</title> shell, while the exact same page 2s later
# has already mounted to the real "Login · Mycorrhizal CRM" -- on a slower
# JS engine (older Firefox) the race is lost every time, not occasionally.
# Poll for the title to move off the static default instead of trusting one
# immediate read.
title=""
root_len=-1
deadline=$(($(date +%s) + 15))
while [ "$(date +%s)" -lt "$deadline" ]; do
  title="$(curl -fsS "${base}/session/${session_id}/title" | python3 -c 'import json,sys; print(json.load(sys.stdin)["value"])')"
  root_len="$(curl -fsS -X POST "${base}/session/${session_id}/execute/sync" -H 'Content-Type: application/json' \
    -d '{"script":"var r = document.getElementById(\"root\"); return r ? r.innerHTML.length : -1;","args":[]}' \
    | python3 -c 'import json,sys; print(json.load(sys.stdin)["value"])')"
  if [ "$title" != "Mycorrhizal CRM" ] && [ -n "$title" ] && [ "$root_len" -gt 0 ]; then
    break
  fi
  sleep 0.5
done
echo "Page title: $title"
echo "#root innerHTML length: $root_len"

if [ "$root_len" -le 0 ]; then
  echo "::error::$kind $browser_version rendered nothing into #root (length $root_len) within 15s -- the app did not load on the documented browser floor"
  exit 1
fi

# A real SPA renders a real page title, not the static index.html title
# alone -- proves React actually mounted and ran, not just that the HTML
# shell was served.
if [ "$title" = "Mycorrhizal CRM" ] || [ -z "$title" ]; then
  echo "::error::$kind $browser_version never got past the static <title> ('$title') within 15s -- the app bundle likely failed to execute"
  exit 1
fi

echo "OK: $kind $browser_version loaded the app (title '$title', #root ${root_len} chars)."

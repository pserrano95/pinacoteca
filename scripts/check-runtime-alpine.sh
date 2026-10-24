#!/bin/sh
# Fail if the runtime FROM is an Alpine release that no longer receives
# security fixes. Dates are Alpine's (https://alpinelinux.org/releases/),
# read as JSON from endoflife.date, which republishes that table.
# Unknown branches and a failed lookup fail closed: a skipped check is not a pass.
set -eu

dockerfile=${1:-Dockerfile}

if [ ! -f "$dockerfile" ]; then
  echo "runtime Alpine check: Dockerfile not found: ${dockerfile}" >&2
  exit 1
fi

runtime=$(grep -E '^FROM[[:space:]]' "$dockerfile" | tail -n 1 || true)
branch=$(printf '%s\n' "$runtime" | sed -n 's/^FROM[[:space:]][[:space:]]*alpine:\([0-9][0-9]*\.[0-9][0-9]*\).*/\1/p')

if [ -z "$branch" ]; then
  echo "runtime Alpine check: the last FROM is not a numbered Alpine release: ${runtime:-<none>}" >&2
  echo "expected FROM alpine:<major>.<minor> (the build stage golang:…-alpine is not the image that ships)" >&2
  exit 1
fi

if ! body=$(curl -fsSL --max-time 30 "https://endoflife.date/api/alpine/${branch}.json"); then
  echo "runtime Alpine check: could not read the end-of-support date for Alpine ${branch}" >&2
  exit 1
fi

eol=$(printf '%s\n' "$body" | sed -n 's/.*"eol":"\([0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]\)".*/\1/p')
if [ -z "$eol" ]; then
  echo "runtime Alpine check: Alpine ${branch} has no end-of-support date; refusing ${runtime}" >&2
  exit 1
fi

today=$(date -u +%Y-%m-%d)
# ISO-8601 dates sort lexicographically. The eol day is already out of support.
earlier=$(printf '%s\n%s\n' "$today" "$eol" | sort | head -n 1)
if [ "$earlier" = "$eol" ]; then
  echo "runtime Alpine ${branch} is out of support since ${eol} (today ${today}): ${runtime}" >&2
  exit 1
fi

echo "runtime Alpine ${branch} is supported until ${eol} (${runtime})"

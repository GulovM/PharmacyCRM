#!/usr/bin/env bash
set -euo pipefail
find backend -type f -name '*.go' -print0 | xargs -0 gofmt -w

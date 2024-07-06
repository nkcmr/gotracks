#!/bin/bash

set -eoux pipefail

current_tag="$(git describe --tags --abbrev=0)"
# datever: https://github.com/nkcmr/datever
next_tag="$(datever increment "$current_tag")"

if [[ "$(git describe --exact-match --tags 2>/dev/null)" != "" ]]; then
  echo "Current commit is already tagged; quiting..."
  exit 1
fi

git tag "$next_tag"

#!/bin/bash

set -eoux pipefail

rm -vf ./img.tar

# https://ko.build/
env KO_DOCKER_REPO=ghcr.io/nkcmr ko \
  build -B \
  --push=false \
  -t latest \
  --tarball ./img.tar \
  .

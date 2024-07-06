#!/bin/bash

set -eoux pipefail

rm -vf ./img.tar

# flag:local do not push, save as .tar
# argparse:start BELOW IS AUTO-GENERATED - DO NOT TOUCH (by: code.nkcmr.net/argparse)
flag_local=""
while [[ $# -gt 0 ]]; do
  case "$(echo "$1" | cut -d= -f1)" in
  -h | --help)
    echo "Usage:"
    echo "  $0 [flags]"
    echo
    echo "Flags:"
    echo "  -h, --help    print this help message"
    echo "  --local       do not push, save as .tar"
    exit 1
    ;;
  --local)
    if [[ $# -eq 1 ]] || [[ "$2" == -* ]]; then
      if [[ "$1" == *=* ]]; then
        flag_local="$(echo "$1" | cut -d= -f2-)"
      else
        flag_local=true
      fi
    else
      shift
      flag_local="$1"
    fi
    ;;
  --)
    shift
    break
    ;;
  -*)
    printf 'Unknown flag "%s"' "$1"
    echo
    exit 1
    ;;
  *)
    echo "$0: error: accepts 0 args, received 1 or more"
    exit 1
    ;;
  esac
  shift
done
# argparse:stop ABOVE CODE IS AUTO-GENERATED - DO NOT TOUCH

build_args=()
build_args+=("-B" "--image-label" "org.opencontainers.image.source=https://github.com/nkcmr/gotracks")

if [[ $flag_local ]]; then
  build_args+=("--push=false" "--tarball" "./img.tar")
  build_args+=("-t" "latest")
else
  current_tag="$(git describe --abbrev=0 --tags)"
  if [[ "$current_tag" == "" ]]; then
    echo "error: current commit is not tagged"
    exit 1
  fi
  build_args+=("-t" "$current_tag")
fi

# https://ko.build/
env KO_DOCKER_REPO=ghcr.io/nkcmr ko build "${build_args[@]}" .

#!/bin/bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/lib/storage_profiles.sh"

got="$(weknora_storage_profile_args minio minio:9000)"
if [ "$got" != "--profile minio" ]; then
    printf 'bundled MinIO profile = %q, want %q\n' "$got" "--profile minio" >&2
    exit 1
fi

got="$(weknora_storage_profile_args minio '')"
if [ "$got" != "--profile minio" ]; then
    printf 'default MinIO profile = %q, want %q\n' "$got" "--profile minio" >&2
    exit 1
fi

got="$(weknora_storage_profile_args minio http://WeKnora-minio:9000)"
if [ "$got" != "--profile minio" ]; then
    printf 'named bundled MinIO profile = %q, want %q\n' "$got" "--profile minio" >&2
    exit 1
fi

got="$(weknora_storage_profile_args minio storage.example.com:9000)"
if [ -n "$got" ]; then
    printf 'remote MinIO unexpectedly enabled local profile: %q\n' "$got" >&2
    exit 1
fi

got="$(weknora_storage_profile_args local minio:9000)"
if [ -n "$got" ]; then
    printf 'local storage unexpectedly enabled MinIO profile: %q\n' "$got" >&2
    exit 1
fi

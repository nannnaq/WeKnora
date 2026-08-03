#!/bin/bash

weknora_storage_profile_args() {
    local storage_type="${1:-local}"
    local endpoint="${2:-}"

    if [ "${storage_type,,}" != "minio" ]; then
        return 0
    fi

    endpoint="${endpoint#http://}"
    endpoint="${endpoint#https://}"
    endpoint="${endpoint%%/*}"
    if [ -z "$endpoint" ]; then
        endpoint="minio:9000"
    fi

    local host="${endpoint%%:*}"
    case "${host,,}" in
        minio|weknora-minio)
            printf '%s\n' '--profile minio'
            ;;
    esac
}

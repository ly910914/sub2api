#!/bin/sh
set -e
if [ "$(id -u)" = "0" ]; then
    mkdir -p /app/data
    chown -R sub2api:sub2api /app/data
    exec gosu sub2api "$0" "$@"
fi
if [ "${1#-}" != "$1" ]; then
    set -- /app/sub2api "$@"
fi
exec "$@"

#!/bin/sh
# Smoke test (T047, constitution gate "starts with a fresh data volume"):
# fresh volume -> healthy -> pages open (no login, feature 007) -> a collector is added -> restart ->
# healthy again and the collector is still there.
# POSIX sh; needs docker and curl. HNE_IMAGE: prebuilt image (skips the build).
# HNE_SMOKE_HOST: where the published port is reachable (default 127.0.0.1).
# HNE_EXPECT_VERSION: if set, /version must report exactly this version (T099).
set -eu

IMAGE=${HNE_IMAGE:-}
HOST=${HNE_SMOKE_HOST:-127.0.0.1}
PORT=${HNE_SMOKE_PORT:-18080}
NAME=hne-smoke-$$
VOL=hne-smoke-$$
BASE=http://$HOST:$PORT

if [ -z "$IMAGE" ]; then
    IMAGE=hne-server:smoke
    docker build -f deploy/Dockerfile -t "$IMAGE" .
fi

cleanup() {
    docker rm -f "$NAME" >/dev/null 2>&1 || true
    docker volume rm "$VOL" >/dev/null 2>&1 || true
}
trap cleanup EXIT

fail() {
    echo "smoke: FAIL: $*" >&2
    docker logs "$NAME" >&2 || true
    exit 1
}

wait_healthy() {
    i=0
    while [ "$(docker inspect --format '{{.State.Health.Status}}' "$NAME")" != healthy ]; do
        i=$((i + 1))
        [ "$i" -le 60 ] || fail "container not healthy after 60s"
        sleep 1
    done
}

status() { # method path [form]
    if [ $# -ge 3 ]; then
        curl -s -o /dev/null -w '%{http_code}' -X "$1" -H "Origin: $BASE" --data "$3" "$BASE$2"
    else
        curl -s -o /dev/null -w '%{http_code}' -X "$1" "$BASE$2"
    fi
}

docker run -d --name "$NAME" -p "$PORT:8080" -v "$VOL:/data" "$IMAGE" --no-builtin-scan >/dev/null
wait_healthy
echo "smoke: healthy on a fresh volume"

version=$(curl -s "$BASE/version")
echo "smoke: /version = $version"
case "$version" in *version*) ;; *) fail "/version did not answer with a version" ;; esac
if [ -n "${HNE_EXPECT_VERSION:-}" ]; then
    case "$version" in *"\"$HNE_EXPECT_VERSION\""*) ;; *) fail "/version is not $HNE_EXPECT_VERSION" ;; esac
fi

[ "$(status GET /)" = 200 ] || fail "the home page should open without a login"
[ "$(status POST /collectors "name=smoke")" = 200 ] || fail "adding a collector failed"
echo "smoke: pages open, collector added"

docker restart "$NAME" >/dev/null
wait_healthy
echo "smoke: healthy after restart"

curl -s "$BASE/collectors" | grep -q ">smoke<" || fail "the collector is gone after restart (data not persisted?)"
echo "smoke: OK"

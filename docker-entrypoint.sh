#!/bin/sh
# Drops root before running the vastiva binary (#37: the container ran as
# root unconditionally, so any bug that escaped the app's own path
# sandboxing had root's full reach over the host filesystem the volumes
# expose).
#
# This still starts as root — PID 1 in the container — because two things
# need it: reading the GPU render node's group, and making /data (and any
# pre-existing files in it, from before this change) writable by whatever
# identity the app is about to drop to. Everything after that runs as an
# unprivileged process; root's capabilities aren't retained.
#
# PUID/PGID, when set, are the *actual* uid/gid the process runs as here —
# not a chown target for a process that stays some other identity. A file
# this process writes is correctly owned from the moment it's created, no
# elevated capability required, which is also why FileOwnership.Apply()'s
# chown in the Go code becomes a same-owner no-op in the common case rather
# than a capability this container needs to be granted. Without PUID/PGID,
# this falls back to a fixed 1000:1000 — no /etc/passwd entry needed, since
# setpriv works with bare numeric ids — still never root, just not matched
# to any particular host identity.
set -e

RUN_UID="${PUID:--1}"
RUN_GID="${PGID:--1}"
if [ "$RUN_UID" = "-1" ] || [ "$RUN_GID" = "-1" ]; then
    RUN_UID=1000
    RUN_GID=1000
fi

mkdir -p /data
chown -R "${RUN_UID}:${RUN_GID}" /data
# Deliberately not /storage or /output: those are the media library, sized
# in the terabytes, and the app already owns per-file ownership on write
# (FileOwnership.Apply) rather than a bulk chown. Recursing over the whole
# library here on every start would be slow and would reset ownership on
# files this container never touched.

# The GPU render node's owning group varies by host and isn't known until
# the device is bind-mounted in, so it's resolved here rather than baked
# into the image. --groups fully replaces the supplementary group list
# (not appends), so this also strips whatever root's own groups were —
# always pass one or the other rather than leaving setpriv's default
# (inherit root's groups unchanged) in the no-GPU case.
GROUPS_ARG="--clear-groups"
for dev in /dev/dri/renderD128 /dev/dri/card0; do
    if [ -e "$dev" ]; then
        RENDER_GID=$(stat -c '%g' "$dev")
        GROUPS_ARG="--groups ${RENDER_GID}"
        break
    fi
done

export HOME=/data

exec setpriv --reuid "${RUN_UID}" --regid "${RUN_GID}" ${GROUPS_ARG} --no-new-privs "$@"

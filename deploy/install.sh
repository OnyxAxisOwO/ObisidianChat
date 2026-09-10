#!/bin/sh
set -eu

release=${1:?release identifier required}
expected_hash=${2:?binary SHA256 required}
case "$release" in ''|*[!0-9]*) echo 'Invalid release identifier' >&2; exit 1 ;; esac
case "$expected_hash" in ''|*[!0-9a-f]*) echo 'Invalid SHA256' >&2; exit 1 ;; esac
test "${#expected_hash}" -eq 64
cd "$(dirname "$0")"
printf '%s  %s\n' "$expected_hash" obsidianchat | sha256sum -c -

if ! id obsidianchat >/dev/null 2>&1; then
  useradd --system --home-dir /data/obsidianchat --shell /usr/sbin/nologin obsidianchat
fi
install -d -m 0755 /opt/obsidianchat/releases
install -d -m 0750 -o root -g obsidianchat /etc/obsidianchat
install -d -m 0700 -o obsidianchat -g obsidianchat /data/obsidianchat
install -d -m 0755 "/opt/obsidianchat/releases/$release"
install -m 0755 obsidianchat "/opt/obsidianchat/releases/$release/obsidianchat"

if ! test -e /etc/obsidianchat/server.env; then
  python3 - <<'PY'
import os, secrets
path = '/etc/obsidianchat/server.env'
fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
with os.fdopen(fd, 'w') as env:
    env.write('OC_ADDR=0.0.0.0:8090\n')
    env.write('OC_DATABASE=/data/obsidianchat/chat.db\n')
    env.write('OC_ORIGIN=http://156.239.242.24:8090\n')
    env.write('OC_SECURE_COOKIE=false\n')
    env.write('OC_SETUP_TOKEN=' + secrets.token_hex(24) + '\n')
PY
fi

previous=$(readlink /opt/obsidianchat/current || true)
if test -f /etc/systemd/system/obsidianchat.service; then
  cp -p /etc/systemd/system/obsidianchat.service "/etc/obsidianchat/service-before-$release"
fi
install -m 0644 obsidianchat.service /etc/systemd/system/obsidianchat.service
ln -s "/opt/obsidianchat/releases/$release" /opt/obsidianchat/current.next
mv -Tf /opt/obsidianchat/current.next /opt/obsidianchat/current
systemd-analyze verify /etc/systemd/system/obsidianchat.service
systemctl daemon-reload
systemctl enable obsidianchat
systemctl restart obsidianchat

healthy=false
for attempt in 1 2 3 4 5 6 7 8 9 10; do
  if curl --max-time 2 -fsS http://127.0.0.1:8090/healthz >/dev/null; then
    healthy=true
    break
  fi
  sleep 1
done
if test "$healthy" != true; then
  systemctl stop obsidianchat
  if test -n "$previous"; then
    ln -s "$previous" /opt/obsidianchat/rollback.next
    mv -Tf /opt/obsidianchat/rollback.next /opt/obsidianchat/current
    if test -f "/etc/obsidianchat/service-before-$release"; then
      cp -p "/etc/obsidianchat/service-before-$release" /etc/systemd/system/obsidianchat.service
      systemctl daemon-reload
    fi
    systemctl start obsidianchat
  fi
  echo 'Health check failed; previous release restored when available' >&2
  exit 1
fi

if command -v ufw >/dev/null 2>&1 && ufw status | grep -q '^Status: active' && grep -q '^OC_ADDR=0\.0\.0\.0:8090$' /etc/obsidianchat/server.env; then
  ufw allow 8090/tcp comment 'Obsidian Chat'
fi
systemctl show obsidianchat -p ActiveState -p SubState -p UnitFileState -p MainPID -p MemoryCurrent
curl --max-time 3 -fsS http://127.0.0.1:8090/api/status

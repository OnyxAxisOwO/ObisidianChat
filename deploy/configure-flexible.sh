#!/bin/sh
set -eu
cd "$(dirname "$0")"
stamp=$(date +%Y%m%d%H%M%S)
caddy_backup="/etc/caddy/Caddyfile.before-chat-flexible-$stamp"
env_backup="/etc/obsidianchat/server.env.before-flexible-$stamp"
candidate="/etc/caddy/Caddyfile.chat-candidate-$stamp"
if grep -q 'chat.onyxaxis.org' /etc/caddy/Caddyfile; then
  echo 'Chat site already exists; inspect before updating' >&2
  exit 1
fi
cp -p /etc/caddy/Caddyfile "$caddy_backup"
cp -p /etc/obsidianchat/server.env "$env_backup"
cat /etc/caddy/Caddyfile > "$candidate"
printf '\n' >> "$candidate"
cat chat-flexible.caddy >> "$candidate"
caddy validate --config "$candidate" --adapter caddyfile

rollback() {
  cp -p "$caddy_backup" /etc/caddy/Caddyfile
  cp -p "$env_backup" /etc/obsidianchat/server.env
  systemctl restart obsidianchat
  systemctl reload caddy
}
trap 'rollback' EXIT
python3 - <<'PY'
from pathlib import Path
import os
path = Path('/etc/obsidianchat/server.env')
values = {'OC_ADDR': '127.0.0.1:8090', 'OC_ORIGIN': 'https://chat.onyxaxis.org', 'OC_SECURE_COOKIE': 'true'}
lines = []
for line in path.read_text().splitlines():
    key = line.split('=', 1)[0]
    lines.append(key + '=' + values.pop(key) if key in values else line)
lines.extend(key + '=' + value for key, value in values.items())
temporary = path.with_suffix('.pending')
fd = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
with os.fdopen(fd, 'w') as file:
    file.write('\n'.join(lines) + '\n')
os.replace(temporary, path)
PY
install -m 0644 "$candidate" /etc/caddy/Caddyfile
systemctl restart obsidianchat
ready=false
for attempt in 1 2 3 4 5 6 7 8 9 10; do
  if curl -fsS --max-time 2 http://127.0.0.1:8090/healthz >/dev/null; then
    ready=true
    break
  fi
  sleep 1
done
test "$ready" = true
systemctl reload caddy
curl -fsS --max-time 5 -H 'Host: chat.onyxaxis.org' -H 'X-Forwarded-Proto: https' http://127.0.0.1/healthz
trap - EXIT
printf '\nCaddy backup: %s\nApp config backup: %s\n' "$caddy_backup" "$env_backup"
systemctl is-active caddy obsidianchat

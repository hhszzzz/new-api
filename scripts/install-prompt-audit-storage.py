#!/usr/bin/env python3
"""Prepare CCS integration; --apply writes it, --activate enables daily archives."""
import argparse
import datetime as dt
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import uuid


def patch_backup(text):
    invocation = '    /usr/local/bin/ccs-prompt-audit-storage.py backup --stage "$STAGE"'
    bounded = '    systemd-run --quiet --wait --pipe --collect -p MemoryHigh=384M -p MemoryMax=512M -p CPUQuota=50% -p RuntimeMaxSec=7200 -p Nice=15 -p IOSchedulingClass=idle /usr/local/bin/ccs-prompt-audit-storage.py backup --stage "$STAGE"'
    if "# prompt-audit independent daily archive" in text:
        text = text.replace(invocation, bounded)
    else:
        dump = '  docker exec newapi-postgres pg_dump -U newapi -d newapi 2>/dev/null | gzip > "$STAGE/newapi-postgres.sql.gz"'
        replacement = '''  # prompt-audit independent daily archive (the existing 03:00 timer is retained)
  if [ -f /etc/ccs-prompt-audit-storage.enabled ]; then
    /usr/local/bin/ccs-prompt-audit-storage.py backup --stage "$STAGE" || { rm -rf "$WORK"; exit 1; }
  else
    docker exec newapi-postgres pg_dump -U newapi -d newapi 2>/dev/null | gzip > "$STAGE/newapi-postgres.sql.gz"
  fi'''
        sanity = 'if [ ! -s "$STAGE/newapi-postgres.sql.gz" ] || [ ! -d "$STAGE/etc/caddy" ]; then'
        revised = 'if { [ ! -s "$STAGE/newapi-postgres.sql.gz" ] && [ ! -s "$STAGE/newapi-postgres.dump" ]; } || [ ! -d "$STAGE/etc/caddy" ]; then'
        if text.count(dump) != 1 or text.count(sanity) != 1:
            raise RuntimeError("the existing CCS backup script differs from the reviewed version")
        text = text.replace(dump, replacement).replace(sanity, revised).replace(invocation, bounded)
    if "# prompt-audit verified transfer adapter" not in text:
        transfer = 'BPC="/usr/local/bin/BaiduPCS-Go"'
        if text.count(transfer) != 1:
            raise RuntimeError("the CCS backup transfer command differs from the reviewed version")
        text = text.replace(transfer, transfer+'''
# prompt-audit verified transfer adapter (other invocations retain their configuration)
if [ -f /etc/ccs-prompt-audit-storage.enabled ] && [ -x /usr/local/bin/ccs-prompt-audit-baidu ]; then
  BPC="/usr/local/bin/ccs-prompt-audit-baidu"
fi''')
    if "# prompt-audit recovery configuration" not in text:
        database = 'if docker ps --format \'{{.Names}}\' | grep -qx "newapi-postgres"; then'
        if text.count(database) != 1:
            raise RuntimeError("the CCS database backup branch differs from the reviewed version")
        text = text.replace(database, '''# prompt-audit recovery configuration (the viewing cache is recreated by import)
if [ -f /etc/ccs-prompt-audit-storage.conf ]; then
  mkdir -p "$STAGE/prompt-audit-host"
  for path in /opt/new-api/docker-compose.override.yml \\
    /usr/local/bin/ccs-backup.sh /usr/local/bin/ccs-backup-notify.sh \\
    /usr/local/bin/ccs-prompt-audit-storage.py /usr/local/bin/ccs-prompt-audit-baidu \\
    /etc/ccs-prompt-audit-storage.conf /etc/ccs-prompt-audit-storage.enabled \\
    /etc/ccs-prompt-audit-baidu /etc/systemd/system/ccs-prompt-audit-* \\
    /etc/systemd/system/ccs-backup.service.d /etc/systemd/system/ccs-backup.timer; do
    [ -e "$path" ] && cp -a --parents "$path" "$STAGE/prompt-audit-host/"
  done
fi
'''+database)
    if "# prompt-audit host maintenance lock" not in text:
        transfer = 'BPC="/usr/local/bin/BaiduPCS-Go"'
        if text.count(transfer) != 1:
            raise RuntimeError("the CCS backup transfer command differs from the reviewed version")
        text = text.replace(transfer, '''# prompt-audit host maintenance lock (one heavy job on this host)
if [ -f /etc/ccs-prompt-audit-storage.conf ]; then
  mkdir -p /opt/new-api/data/prompt-audit-maintenance
  exec 9>/opt/new-api/data/prompt-audit-maintenance/host-backup.lock
  if ! flock -n 9; then
    echo "CCS backup postponed: prompt audit maintenance is running" >&2
    exit 0
  fi
fi
'''+transfer)
    return text


def patch_compose(text):
    key = "      PROMPT_AUDIT_IMPORT_DIR: /data/prompt-audit-maintenance/imports"
    pattern = re.compile(r"(^  new-api:\n(?:(?!^  \S).)*?^    environment:\n)", re.M | re.S)
    matches = list(pattern.finditer(text))
    if len(matches) != 1:
        raise RuntimeError("the new-api environment mapping could not be identified")
    begin = matches[0].end()
    end = re.search(r"^    \S|^  \S", text[begin:], re.M)
    finish = begin+end.start() if end else len(text)
    section = text[begin:finish]
    existing = re.compile(r"^      PROMPT_AUDIT_IMPORT_DIR:.*$", re.M)
    section = existing.sub(key, section) if existing.search(section) else key+"\n"+section
    return text[:begin]+section+text[finish:]


def unit(command):
    return f'''[Unit]
Description=New-API prompt audit {command}
After=docker.service

[Service]
Type=oneshot
UMask=0077
Nice=15
IOSchedulingClass=idle
MemoryHigh=384M
MemoryMax=512M
CPUQuota=50%
ExecStart=/usr/local/bin/ccs-prompt-audit-storage.py {command}
TimeoutStartSec=7200
'''


def timer(command, interval):
    return f'''[Unit]
Description=New-API prompt audit {command} every {interval} minutes

[Timer]
OnBootSec={interval}min
OnUnitActiveSec={interval}min
Unit=ccs-prompt-audit-{command}.service

[Install]
WantedBy=timers.target
'''


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--root", default="/", help="alternate root for a review fixture")
    parser.add_argument("--apply", action="store_true")
    parser.add_argument("--activate", action="store_true", help="after migration and application restart")
    args = parser.parse_args()
    root = Path(args.root)
    changes = {
        "usr/local/bin/ccs-backup.sh": patch_backup((root/"usr/local/bin/ccs-backup.sh").read_text()),
        "opt/new-api/docker-compose.override.yml": patch_compose((root/"opt/new-api/docker-compose.override.yml").read_text()),
        "usr/local/bin/ccs-prompt-audit-storage.py": Path(__file__).with_name("prompt-audit-storage.py").read_text(),
    }
    config = "etc/ccs-prompt-audit-storage.conf"
    if not (root/config).exists():
        changes[config] = "HOST_ROOT=/opt/new-api/data/prompt-audit-maintenance\nCONTAINER_ROOT=/data/prompt-audit-maintenance\nREMOTE_ROOT=/ccs-backup/prompt-audit\nAGE_KEY=/root/.config/age/keys.txt\n"
    for command, interval in (("import-jobs", 1), ("disk-check", 5)):
        changes[f"etc/systemd/system/ccs-prompt-audit-{command}.service"] = unit(command)
        changes[f"etc/systemd/system/ccs-prompt-audit-{command}.timer"] = timer(command, interval)
    changes["etc/systemd/system/ccs-backup.service.d/prompt-audit.conf"] = "[Service]\nMemoryHigh=384M\nMemoryMax=512M\nCPUQuota=50%\nNice=15\nIOSchedulingClass=idle\nTimeoutStartSec=7200\n"
    subprocess.run(["bash", "-n"], input=changes["usr/local/bin/ccs-backup.sh"], text=True, capture_output=True, check=True)
    if args.activate:
        if root != Path("/") or not args.apply:
            parser.error("--activate requires --apply on the host")
        response = subprocess.run(["docker", "exec", "newapi-app", "/new-api", "prompt-audit", "stats"], capture_output=True, check=True, text=True)
        stats = json.loads(response.stdout.splitlines()[-1])
        if not stats["shared_enabled"] or stats["unmigrated"] or stats["legacy_bytes"]:
            raise RuntimeError("complete the second migration pass after restarting the application")
        changes["etc/ccs-prompt-audit-storage.enabled"] = "enabled after migration\n"
    print(json.dumps({"apply": args.apply, "activate": args.activate, "files": list(changes), "backup_time": "03:00 Asia/Shanghai (existing timer)"}))
    if not args.apply:
        return
    stamp = dt.datetime.now(dt.timezone.utc).strftime("%Y%m%dT%H%M%SZ")
    backup = root/("opt/new-api/backups/prompt-audit-host-"+stamp+"-"+uuid.uuid4().hex[:8])
    backup.mkdir(parents=True, mode=0o700)
    for relative in changes:
        path = root/relative
        if path.exists():
            copy = backup/relative
            copy.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
            shutil.copy2(path, copy)
    for relative, content in changes.items():
        path = root/relative
        path.parent.mkdir(parents=True, exist_ok=True)
        temporary = path.with_name(path.name+".prompt-audit.tmp")
        temporary.write_text(content)
        os.chmod(temporary, 0o700 if relative.startswith("usr/local/bin/") else 0o600 if relative.startswith("etc/ccs-") else 0o644)
        temporary.replace(path)
    if root == Path("/"):
        subprocess.run(["systemctl", "daemon-reload"], check=True)
        subprocess.run(["systemctl", "enable", "--now", "ccs-prompt-audit-import-jobs.timer", "ccs-prompt-audit-disk-check.timer"], check=True)
    print(json.dumps({"configuration_backup": str(backup), "application_recreate_required": True}))


if __name__ == "__main__":
    main()

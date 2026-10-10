#!/usr/bin/env python3
"""Host adapter for independent audit archives, encrypted imports and df alerts."""
import argparse
import datetime as dt
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import shlex
import shutil
import signal
import subprocess
import sys
import tempfile
import time
import uuid
from urllib.parse import parse_qsl, unquote, urlsplit

DEFAULTS = {
    "APP_CONTAINER": "newapi-app", "APP_BINARY": "/new-api",
    "DB_CONTAINER": "newapi-postgres", "DB_USER": "newapi", "DB_NAME": "newapi",
    "HOST_ROOT": "/opt/new-api/data/prompt-audit-maintenance",
    "CONTAINER_ROOT": "/data/prompt-audit-maintenance",
    "AGE_KEY": "/root/.config/age/keys.txt", "BAIDU_CLI": "/usr/local/bin/BaiduPCS-Go",
    "REMOTE_ROOT": "/ccs-backup/prompt-audit",
    "NOTIFY": "/usr/local/bin/ccs-backup-notify.sh", "DISK_PATH": "/opt/new-api",
}
NAME = re.compile(r"^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,199}$")
LIMIT = 2 << 30
VOLUME_BYTES = 512 << 20
MAINTENANCE_FREE_BYTES = 768 << 20


def configuration():
    values = dict(DEFAULTS)
    path = Path(os.environ.get("PROMPT_AUDIT_HOST_CONFIG", "/etc/ccs-prompt-audit-storage.conf"))
    if path.exists():
        for line in path.read_text().splitlines():
            key, separator, value = line.partition("=")
            if separator and key.strip() in values:
                values[key.strip()] = value.strip().strip('"').strip("'")
    for key in values:
        values[key] = os.environ.get("PROMPT_AUDIT_"+key, values[key])
    return values


def write_json(path, value):
    path = Path(path)
    temporary = path.with_suffix(path.suffix+".tmp")
    with temporary.open("w") as file:
        os.chmod(temporary, 0o600)
        json.dump(value, file, ensure_ascii=False)
    temporary.replace(path)


def file_digest(path):
    digest = hashlib.sha256()
    with Path(path).open("rb") as file:
        for data in iter(lambda: file.read(1 << 20), b""):
            digest.update(data)
    return digest.hexdigest()


def run(arguments, **kwargs):
    kwargs.setdefault("env", dict(os.environ, GOMAXPROCS="1", GOMEMLIMIT="256MiB"))
    result = subprocess.run(arguments, capture_output=True, **kwargs)
    if result.returncode:
        raise RuntimeError("maintenance command failed: "+Path(arguments[0]).name)
    return result.stdout


def audit_command(config, *arguments, input_stream=False):
    return maintenance_command(config, "cli", *arguments)


def maintenance_command(config, role, *arguments):
    root = Path(config["HOST_ROOT"])/"workers"
    root.mkdir(parents=True, exist_ok=True, mode=0o700)
    values = {key: config[key] for key in DEFAULTS}
    path = root/("worker-config-"+uuid.uuid4().hex+".json")
    write_json(path, values)
    return [sys.executable, str(Path(__file__).resolve()), "worker", "--role", role,
            "--worker-config", str(path), "--", *arguments]


def cleanup_workers(config, invocation=None, worker_name=None):
    root = Path(config["HOST_ROOT"])/"workers"
    if not root.exists():
        return
    for marker in root.glob("audit-worker-*.json"):
        if marker.is_symlink() or not re.fullmatch(r"audit-worker-[a-f0-9]{32}\.json", marker.name):
            continue
        record = json.loads(marker.read_text())
        name = marker.stem
        if worker_name and worker_name != name:
            continue
        if invocation:
            if record.get("invocation") != invocation:
                continue
        elif not worker_name and Path("/proc/"+str(record["pid"])).exists():
            continue
        inspected = subprocess.run(["docker", "inspect", name], capture_output=True, timeout=15)
        if inspected.returncode == 0:
            container = json.loads(inspected.stdout)[0]
            if (container["Config"].get("Labels") or {}).get("new-api.prompt-audit-worker") != name:
                continue
            subprocess.run(["docker", "rm", "-f", container["Id"]], check=True, timeout=20,
                           stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        elif b"no such object" not in inspected.stderr.lower() and b"no such container" not in inspected.stderr.lower():
            raise RuntimeError("Docker worker inspection failed; cleanup marker retained")
        (root/(name+".env")).unlink(missing_ok=True)
        marker.unlink()
    for path in root.glob("worker-config-*.json"):
        if not path.is_symlink() and re.fullmatch(r"worker-config-[a-f0-9]{32}\.json", path.name) and time.time()-path.stat().st_mtime > 86400:
            path.unlink()


def maintenance_worker(config, role, arguments):
    app = json.loads(run(["docker", "inspect", config["APP_CONTAINER"]]))[0]
    database = json.loads(run(["docker", "inspect", config["DB_CONTAINER"]]))[0]
    networks = sorted(set(app["NetworkSettings"]["Networks"]) & set(database["NetworkSettings"]["Networks"]))
    if not networks:
        raise RuntimeError("the application and database have no shared maintenance network")
    environment = dict(item.split("=", 1) for item in app["Config"]["Env"] if "=" in item)
    values = {key: environment[key] for key in ("SQL_DSN", "SQLITE_PATH") if key in environment}
    image, memory = app["Image"], "512m"
    entrypoint, command = "nice", ["-n", "15", config["APP_BINARY"], "prompt-audit", *arguments]
    if role == "pg-dump":
        dsn = environment.get("SQL_DSN", "")
        parsed = urlsplit(dsn)
        if parsed.scheme in ("postgres", "postgresql"):
            settings = dict(host=parsed.hostname or config["DB_CONTAINER"], port=str(parsed.port or 5432),
                            user=unquote(parsed.username or config["DB_USER"]),
                            password=unquote(parsed.password or ""), dbname=unquote(parsed.path.lstrip("/")))
            settings.update(parse_qsl(parsed.query))
        else:
            settings = dict(item.split("=", 1) for item in shlex.split(dsn) if "=" in item)
        if not settings.get("host") or not settings.get("dbname"):
            raise RuntimeError("the application PostgreSQL connection could not be identified")
        keys = {"host": "PGHOST", "hostaddr": "PGHOSTADDR", "port": "PGPORT", "user": "PGUSER",
                "password": "PGPASSWORD", "dbname": "PGDATABASE", "sslmode": "PGSSLMODE",
                "sslrootcert": "PGSSLROOTCERT", "sslcert": "PGSSLCERT", "sslkey": "PGSSLKEY",
                "sslcrl": "PGSSLCRL", "sslcrldir": "PGSSLCRLDIR", "channel_binding": "PGCHANNELBINDING",
                "ssl_min_protocol_version": "PGSSLMINPROTOCOLVERSION", "ssl_max_protocol_version": "PGSSLMAXPROTOCOLVERSION",
                "connect_timeout": "PGCONNECT_TIMEOUT", "target_session_attrs": "PGTARGETSESSIONATTRS",
                "options": "PGOPTIONS"}
        values = {keys[key]: value for key, value in settings.items() if key in keys}
        image, memory, entrypoint = database["Image"], "256m", "pg_dump"
        command = ["-Fc", "-Z", "1", "--no-owner", "--no-privileges", *arguments]
    if any("\n" in value or "\r" in value for value in values.values()):
        raise RuntimeError("maintenance environment contains an unsupported line break")
    root = Path(config["HOST_ROOT"])/"workers"
    root.mkdir(parents=True, exist_ok=True, mode=0o700)
    name = "audit-worker-"+uuid.uuid4().hex
    environment_file, marker = root/(name+".env"), root/(name+".json")
    with environment_file.open("w") as file:
        os.chmod(environment_file, 0o600)
        file.write("".join(key+"="+value+"\n" for key, value in values.items()))
    invocation = os.environ.get("INVOCATION_ID", "")
    write_json(marker, {"pid": os.getpid(), "invocation": invocation})
    arguments = ["docker", "run", "--rm", "--log-driver", "none", "--name", name,
                 "--label", "new-api.prompt-audit-worker="+name, "--cpus", "0.35",
                 "--memory", memory, "--memory-swap", memory, "--network", networks[0],
                 "--env-file", str(environment_file), "-e", "GOMAXPROCS=1", "-e", "GOMEMLIMIT=256MiB"]
    if role == "cli":
        arguments.append("-i")
    for mount in app.get("Mounts", []):
        if mount["Type"] not in ("bind", "volume"):
            continue
        source = mount["Name"] if mount["Type"] == "volume" else mount["Source"]
        mode = "rw" if role == "cli" and mount.get("RW", False) else "ro"
        arguments.extend(["--volume", source+":"+mount["Destination"]+":"+mode])
    arguments.extend(["--entrypoint", entrypoint, image, *command])

    def interrupted(signum, frame):
        raise SystemExit(128+signum)

    previous = {sig: signal.signal(sig, interrupted) for sig in (signal.SIGTERM, signal.SIGINT)}
    process = None
    try:
        process = subprocess.Popen(arguments)
        status = process.wait()
        if status:
            raise RuntimeError("bounded maintenance container failed")
    finally:
        if process is not None and process.poll() is None:
            process.terminate()
            try:
                process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait()
        cleanup_workers(config, worker_name=name)
        for sig, handler in previous.items():
            signal.signal(sig, handler)


def cli(config, *arguments):
    output = run(audit_command(config, *arguments))
    for line in reversed(output.decode("utf-8", "replace").splitlines()):
        try:
            return json.loads(line)
        except ValueError:
            pass
    raise RuntimeError("invalid maintenance response")


def container_path(config, path):
    relative = Path(path).relative_to(config["HOST_ROOT"])
    return str(Path(config["CONTAINER_ROOT"])/relative)


def notify(config, level, message):
    if Path(config["NOTIFY"]).is_file():
        subprocess.run([config["NOTIFY"], level, message], check=False, timeout=25, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)


def remote_download(config, remote, directory, name, size, checksum):
    if not remote.startswith(config["REMOTE_ROOT"].rstrip("/")+"/") or not NAME.fullmatch(name):
        raise RuntimeError("invalid archive remote path")
    # The client can return success when metadata lookup queued no downloads.
    # Each retry gets a fresh directory so an incomplete file cannot be skipped.
    for attempt in range(3):
        with tempfile.TemporaryDirectory(prefix="download-", dir=directory) as work:
            try:
                run([config["BAIDU_CLI"], "download", "-p", "1", "-l", "1", "--saveto", work, "--retry", "2", remote], timeout=max(900, size//(2 << 20)+900))
                matches = list(Path(work).rglob(name))
                if len(matches) == 1 and matches[0].stat().st_size == size and file_digest(matches[0]) == checksum:
                    destination = Path(directory)/name
                    matches[0].replace(destination)
                    return destination
            except (RuntimeError, subprocess.SubprocessError):
                pass
        if attempt < 2:
            time.sleep(2*(attempt+1))
    raise RuntimeError("remote volume verification failed after three attempts")


def upload_verified(config, path, remote):
    size, checksum = path.stat().st_size, file_digest(path)
    for attempt in range(3):
        try:
            run([config["BAIDU_CLI"], "upload", "-p", "1", "-l", "1", "--norapid", "--policy", "overwrite", str(path), remote.rsplit("/", 1)[0]+"/"], timeout=max(900, size//(2 << 20)+900))
        except (RuntimeError, subprocess.SubprocessError):
            # A timed-out final response can follow a committed remote upload.
            # Only downloading the expected bytes can prove it completed.
            pass
        try:
            with tempfile.TemporaryDirectory(prefix="verify-", dir=path.parent) as directory:
                remote_download(config, remote, directory, path.name, size, checksum)
            return {"name": path.name, "bytes": size, "sha256": checksum, "remote_path": remote}
        except (RuntimeError, subprocess.SubprocessError):
            if attempt < 2:
                time.sleep(5*(attempt+1))
    raise RuntimeError("remote volume verification failed after three upload attempts")


def decrypt_process(config, paths):
    source = subprocess.Popen(["cat", *map(str, paths)], stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
    decrypt = subprocess.Popen(["age", "-d", "-i", config["AGE_KEY"]], stdin=source.stdout, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
    source.stdout.close()
    return source, decrypt


def import_encrypted(config, paths, expected_digest):
    # Validate the entire age stream before any viewing-cache transaction can
    # commit. No decrypted archive is written to disk, even for large day packs.
    source, decrypt = decrypt_process(config, paths)
    digest = hashlib.sha256()
    for data in iter(lambda: decrypt.stdout.read(1 << 20), b""):
        digest.update(data)
    decrypt.stdout.close()
    decrypt_status, source_status = decrypt.wait(), source.wait()
    if decrypt_status or source_status or digest.hexdigest() != expected_digest:
        raise RuntimeError("encrypted archive integrity check failed")
    source, decrypt = decrypt_process(config, paths)
    with tempfile.TemporaryFile() as output:
        imported = subprocess.run(audit_command(config, "import", "/dev/stdin", input_stream=True), stdin=decrypt.stdout, stdout=output, stderr=subprocess.DEVNULL)
        decrypt.stdout.close()
        if imported.returncode:
            if decrypt.poll() is None:
                decrypt.kill()
            if source.poll() is None:
                source.kill()
        decrypt_status, source_status = decrypt.wait(), source.wait()
        if imported.returncode or decrypt_status or source_status:
            raise RuntimeError("archive import rejected; check the cache capacity and package format")


def replenish_previous_packages(config, work):
    for item in cli(config, "needed"):
        paths = []
        with tempfile.TemporaryDirectory(prefix="previous-", dir=work) as directory:
            for volume in item["volumes"]:
                paths.append(remote_download(config, volume["remote_path"], directory, volume["name"], volume["bytes"], volume["sha256"]))
            import_encrypted(config, paths, item["archive"]["digest"])


def backup(config, stage, defer_cleanup=False):
    root = Path(config["HOST_ROOT"])
    root.mkdir(parents=True, exist_ok=True, mode=0o700)
    work = root/("export-"+uuid.uuid4().hex)
    work.mkdir(mode=0o700)
    release, state, fifo = work/"release", work/"snapshot.json", work/"archive.pipe"
    exporter = encrypt = None
    try:
        replenish_previous_packages(config, work)
        if shutil.disk_usage(root).free < (1536 << 20):
            raise RuntimeError("insufficient space for a verified archive volume and database metadata dump")
        os.mkfifo(fifo, 0o600)
        day = dt.datetime.now(dt.timezone(dt.timedelta(hours=8))).strftime("%Y-%m-%d")
        prefix = "audit-"+day+"-"+uuid.uuid4().hex
        recipient = run(["age-keygen", "-y", config["AGE_KEY"]]).decode().strip()
        with (work/"export.log").open("wb") as export_log:
            pipe_fd = os.open(fifo, os.O_RDONLY | os.O_NONBLOCK)
            exporter = subprocess.Popen(audit_command(config, "export", "--out", container_path(config, fifo), "--day", day, "--snapshot-state", container_path(config, state), "--release-file", container_path(config, release)), stdout=export_log, stderr=export_log)
            deadline = time.monotonic()+120
            while not state.exists():
                if exporter.poll() is not None or time.monotonic() > deadline:
                    os.close(pipe_fd)
                    raise RuntimeError("archive exporter could not open its database snapshot")
                time.sleep(0.1)
            os.set_blocking(pipe_fd, True)
            with os.fdopen(pipe_fd, "rb") as source:
                encrypt = subprocess.Popen(["age", "-r", recipient], stdin=source, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
                volumes = []
                for number in range(1, 10001):
                    path = work/(prefix+f".tar.zst.age.part-{number:06d}")
                    count = 0
                    with path.open("wb") as file:
                        os.chmod(path, 0o600)
                        while count < VOLUME_BYTES:
                            data = encrypt.stdout.read(min(1 << 16, VOLUME_BYTES-count))
                            if not data:
                                break
                            file.write(data); count += len(data)
                    if not count:
                        path.unlink(); break
                    volumes.append(upload_verified(config, path, config["REMOTE_ROOT"].rstrip("/")+"/"+path.name))
                    path.unlink()
                    if count < VOLUME_BYTES:
                        break
                encrypt.stdout.close()
                if encrypt.wait():
                    raise RuntimeError("archive encryption failed")
            deadline = time.monotonic()+120
            snapshot = json.loads(state.read_text())
            while snapshot["status"] == "writing" and exporter.poll() is None and time.monotonic() < deadline:
                time.sleep(0.1)
                snapshot = json.loads(state.read_text())
            if snapshot["status"] != "ready" or not snapshot["recovery_complete"] or not snapshot["snapshot"]:
                raise RuntimeError("archive recovery coverage is incomplete")
            exclusions = ["--exclude-table-data="+table for table in snapshot["exclude_table_data"]]
            dump = Path(stage)/"newapi-postgres.dump"
            with dump.open("wb") as file:
                os.chmod(dump, 0o600)
                result = subprocess.run(maintenance_command(config, "pg-dump", "--snapshot="+snapshot["snapshot"], *exclusions), stdout=file, stderr=subprocess.DEVNULL)
            if result.returncode or not dump.stat().st_size:
                raise RuntimeError("consistent database snapshot backup failed")
            release.touch(mode=0o600)
            if exporter.wait(timeout=60):
                raise RuntimeError("archive catalog could not be recorded")
        descriptor = {"format": 1, "archive": snapshot["archive"], "volumes": volumes}
        manifest = work/(prefix+".volumes.json")
        write_json(manifest, descriptor)
        upload_verified(config, manifest, config["REMOTE_ROOT"].rstrip("/")+"/"+manifest.name)
        proof = work/"verified-volumes.json"
        write_json(proof, volumes)
        shutil.copy2(manifest, Path(stage)/"prompt-audit-day.json")
        if not defer_cleanup:
            cli(config, "verify", "--id", descriptor["archive"]["id"], "--digest", descriptor["archive"]["digest"], "--volumes", container_path(config, proof))
            cli(config, "cleanup")
        print(json.dumps({"archive": descriptor["archive"]["id"], "volumes": len(volumes), "recovery_complete": True, "acknowledged": not defer_cleanup}))
    finally:
        release.touch(mode=0o600)
        if encrypt and encrypt.poll() is None:
            encrypt.kill(); encrypt.wait()
        if exporter and exporter.poll() is None:
            try:
                exporter.wait(timeout=10)
            except subprocess.TimeoutExpired:
                exporter.terminate()
                try:
                    exporter.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    exporter.kill(); exporter.wait()
        cleanup_workers(config)
        shutil.rmtree(work)


def import_jobs(config):
    root = Path(config["HOST_ROOT"])/"imports"
    root.mkdir(parents=True, exist_ok=True, mode=0o700)
    now = time.time()
    upload_lock = root/".uploading"
    if upload_lock.exists() and now-upload_lock.stat().st_mtime > 86400:
        shutil.rmtree(upload_lock)
    for directory in sorted(root.iterdir()):
        if not directory.is_dir() or directory.name.startswith("."):
            continue
        ready, status = directory/"ready.json", directory/"status.json"
        if not ready.exists():
            if now-directory.stat().st_mtime > 86400:
                shutil.rmtree(directory)
            continue
        if status.exists():
            previous = json.loads(status.read_text())
            if previous.get("status") in ("done", "failed"):
                if now-status.stat().st_mtime > 86400:
                    shutil.rmtree(directory)
                continue
            if now-directory.stat().st_mtime > 86400:
                shutil.rmtree(directory)
                continue
        job = json.loads(ready.read_text())
        job["status"] = "running"; write_json(status, job)
        try:
            files = {entry["name"]: entry for entry in job["files"]}
            manifests = [name for name in files if name.endswith(".volumes.json")]
            if not manifests:
                raise RuntimeError("the downloaded volume manifest is required")
            total = sum(entry["bytes"] for entry in files.values())
            if total > LIMIT:
                raise RuntimeError("archive upload exceeds 2 GiB")
            for name, entry in files.items():
                path = directory/name
                if not NAME.fullmatch(name) or path.is_symlink() or path.stat().st_size != entry["bytes"] or file_digest(path) != entry["sha256"]:
                    raise RuntimeError("uploaded archive checksum mismatch")
            used = set(manifests)
            packages = []
            for name in manifests:
                if files[name]["bytes"] > 1 << 20:
                    raise RuntimeError("invalid volume manifest")
                descriptor = json.loads((directory/name).read_text())
                if descriptor.get("format") != 1 or not descriptor.get("volumes"):
                    raise RuntimeError("invalid volume manifest")
                paths = []
                for volume in descriptor["volumes"]:
                    entry = files.get(volume["name"])
                    if not entry or entry["bytes"] != volume["bytes"] or entry["sha256"] != volume["sha256"]:
                        raise RuntimeError("missing or damaged archive volume")
                    paths.append(directory/volume["name"]); used.add(volume["name"])
                packages.append((paths, descriptor["archive"]["digest"]))
            if used != set(files):
                raise RuntimeError("unrecognized extra archive files")
            for paths, digest in packages:
                import_encrypted(config, paths, digest)
            job["status"] = "done"
        except (OSError, ValueError, KeyError, RuntimeError, subprocess.SubprocessError) as error:
            job["status"], job["message"] = "failed", str(error)
        write_json(status, job)
        for entry in job["files"]:
            if NAME.fullmatch(entry["name"]):
                (directory/entry["name"]).unlink(missing_ok=True)


def disk_alert_decision(state, usage, now):
    if usage < 90:
        state["armed"] = True
    if usage >= 95 and (state.get("armed", True) or now-state.get("last_alert", 0) >= 86400):
        state["armed"], state["last_alert"] = False, now
        return True
    return False


def disk_check(config):
    root = Path(config["HOST_ROOT"]); root.mkdir(parents=True, exist_ok=True, mode=0o700)
    state_path = root/"disk-alert.json"
    state = json.loads(state_path.read_text()) if state_path.exists() else {"armed": True}
    lines = run(["df", "-P", "-B1", config["DISK_PATH"]]).decode().splitlines()
    columns = lines[-1].split(); usage, available = int(columns[-2].rstrip("%")), int(columns[3])
    now = int(time.time())
    detail = "正文统计暂不可用；归档积压暂不可用"
    try:
        stats = cli(config, "stats")
        body = stats["stored_bytes"]+stats["legacy_bytes"]
        previous = state.get("sample", {"time": now, "body": body})
        growth = max(0, body-previous["body"])*86400/max(1, now-previous["time"])
        state["sample"] = {"time": now, "body": body}
        detail = f"正文逻辑占用 {body/(1<<30):.2f} GiB；近期增长约 {growth/(1<<30):.2f} GiB/天；待归档 {stats['archive_backlog']} 条"
    except (RuntimeError, KeyError, ValueError, subprocess.SubprocessError):
        pass
    if disk_alert_decision(state, usage, now):
        notify(config, "warn", f"New-API 磁盘使用率 {usage}%：剩余 {available/(1<<30):.2f} GiB；{detail}")
    write_json(state_path, state)


def maintenance_resources_available():
    memory = Path("/proc/meminfo")
    if memory.exists():
        for line in memory.read_text().splitlines():
            if line.startswith("MemAvailable:") and int(line.split()[1])*1024 < MAINTENANCE_FREE_BYTES:
                return False
    return os.getloadavg()[0] < max(1, os.cpu_count() or 1)*1.5


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("command", choices=["backup", "import-jobs", "disk-check", "worker", "cleanup-workers", "database-backup"])
    parser.add_argument("--stage")
    parser.add_argument("--defer-cleanup", action="store_true", help="retain local bodies while validating the initial recovery backup")
    parser.add_argument("--role", choices=["cli", "pg-dump"])
    parser.add_argument("--worker-config")
    args, arguments = parser.parse_known_args()
    config = configuration()
    if args.command == "worker" and args.worker_config:
        values = json.loads(Path(args.worker_config).read_text())
        config.update({key: value for key, value in values.items() if key in DEFAULTS})
    root = Path(config["HOST_ROOT"]); root.mkdir(parents=True, exist_ok=True, mode=0o700)
    if args.command == "worker":
        if not args.role:
            parser.error("worker requires --role")
        try:
            maintenance_worker(config, args.role, arguments[1:] if arguments[:1] == ["--"] else arguments)
        finally:
            if args.worker_config:
                Path(args.worker_config).unlink(missing_ok=True)
        return
    if arguments:
        parser.error("unexpected maintenance arguments")
    if args.command == "cleanup-workers":
        cleanup_workers(config, os.environ.get("INVOCATION_ID") or None)
        return
    cleanup_workers(config)
    if args.command == "database-backup":
        if not args.stage:
            parser.error("database-backup requires --stage")
        path = Path(args.stage)/"newapi-postgres.dump"
        if not maintenance_resources_available() or shutil.disk_usage(root).free < 1536 << 20:
            raise RuntimeError("full database backup requires host headroom")
        process = None
        try:
            with path.open("wb") as file:
                os.chmod(path, 0o600)
                process = subprocess.Popen(maintenance_command(config, "pg-dump"), stdout=file, stderr=subprocess.DEVNULL)
                while process.poll() is None:
                    if shutil.disk_usage(root).free < 1536 << 20:
                        raise RuntimeError("full database backup stopped before disk headroom was exhausted")
                    time.sleep(1)
            if process.returncode or not path.stat().st_size:
                raise RuntimeError("bounded full database backup failed")
            if shutil.disk_usage(root).free < path.stat().st_size+(1536 << 20):
                raise RuntimeError("insufficient space to encrypt the complete core backup")
        finally:
            if process is not None and process.poll() is None:
                process.terminate()
                try:
                    process.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    process.kill(); process.wait()
            cleanup_workers(config)
        return
    # Backup and viewing imports share one heavy-task lock; disk alerts still run.
    lock_name = "disk-check.lock" if args.command == "disk-check" else "body-maintenance.lock"
    with (root/lock_name).open("w") as lock, (root/"host-backup.lock").open("w") as host_lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            if args.command == "import-jobs":
                # The core backup holds this lock through its final upload;
                # its inner archive command already holds the body lock.
                fcntl.flock(host_lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            if args.command == "backup":
                raise RuntimeError("audit body maintenance is already running; backup postponed")
            return
        try:
            if args.command != "disk-check":
                now = time.time()
                for previous in root.glob("export-*"):
                    if re.fullmatch(r"export-[a-f0-9]{32}", previous.name) and previous.is_dir() and not previous.is_symlink() and now-previous.stat().st_mtime > 86400:
                        shutil.rmtree(previous)
            if args.command != "disk-check" and not maintenance_resources_available():
                if args.command == "backup":
                    raise RuntimeError("host memory or load has insufficient headroom; original bodies retained")
                return
            if args.command == "backup":
                if not args.stage:
                    parser.error("backup requires --stage")
                backup(config, args.stage, args.defer_cleanup)
            elif args.command == "import-jobs":
                import_jobs(config)
            else:
                disk_check(config)
        except Exception as error:
            if args.command == "backup":
                notify(config, "fail", "提示词日包归档失败，七天清理已受保护："+str(error))
            raise


if __name__ == "__main__":
    os.nice(max(0, 15-os.nice(0)))
    main()

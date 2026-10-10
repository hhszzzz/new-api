#!/usr/bin/env python3
import importlib.util
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import Mock, patch


def load_script(name, filename):
    spec = importlib.util.spec_from_file_location(name, Path(__file__).with_name(filename))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


storage = load_script("audit_storage", "prompt-audit-storage.py")
installer = load_script("audit_installer", "install-prompt-audit-storage.py")


class PromptAuditHostContracts(unittest.TestCase):
    def test_busy_host_defers_maintenance_without_changing_jobs(self):
        with patch.object(storage.os, "getloadavg", return_value=(8, 3, 1)), patch.object(storage.os, "cpu_count", return_value=3):
            self.assertFalse(storage.maintenance_resources_available())
        memory = Mock()
        memory.exists.return_value = True
        memory.read_text.return_value = "MemAvailable: 262144 kB\n"
        with patch.object(storage, "Path", return_value=memory), patch.object(storage.os, "getloadavg", return_value=(0, 0, 0)), patch.object(storage.os, "cpu_count", return_value=3):
            self.assertFalse(storage.maintenance_resources_available())
        with tempfile.TemporaryDirectory() as directory:
            config = dict(storage.DEFAULTS, HOST_ROOT=directory)
            expired = Path(directory)/("export-"+"a"*32)
            expired.mkdir(); (expired/"day.age").write_bytes(b"staged encrypted volume")
            os.utime(expired, (1, 1))
            recent = Path(directory)/("export-"+"b"*32); recent.mkdir()
            unrelated = Path(directory)/"export-user-files"; unrelated.mkdir()
            os.utime(unrelated, (1, 1))
            with patch.object(storage, "configuration", return_value=config), patch.object(storage, "maintenance_resources_available", return_value=False), patch.object(storage, "import_jobs") as imported, patch("sys.argv", ["maintenance", "import-jobs"]):
                storage.main()
            imported.assert_not_called()
            self.assertFalse(expired.exists())
            self.assertTrue(recent.exists())
            self.assertTrue(unrelated.exists())
            with patch.object(storage, "configuration", return_value=config), patch.object(storage, "maintenance_resources_available", return_value=False), patch.object(storage, "backup") as backup, patch.object(storage, "notify") as notify, patch("sys.argv", ["maintenance", "backup", "--stage", directory]):
                with self.assertRaisesRegex(RuntimeError, "original bodies retained"):
                    storage.main()
            backup.assert_not_called()
            self.assertEqual(notify.call_count, 1)

    def test_backup_and_import_share_a_lock_but_disk_checks_continue(self):
        with tempfile.TemporaryDirectory() as directory:
            config = dict(storage.DEFAULTS, HOST_ROOT=directory)
            with (Path(directory)/"body-maintenance.lock").open("w") as lock:
                storage.fcntl.flock(lock, storage.fcntl.LOCK_EX | storage.fcntl.LOCK_NB)
                with patch.object(storage, "configuration", return_value=config), patch.object(storage, "import_jobs") as imported, patch("sys.argv", ["maintenance", "import-jobs"]):
                    storage.main()
                imported.assert_not_called()
                with patch.object(storage, "configuration", return_value=config), patch.object(storage, "backup") as backup, patch("sys.argv", ["maintenance", "backup", "--stage", directory]):
                    with self.assertRaisesRegex(RuntimeError, "already running"):
                        storage.main()
                backup.assert_not_called()
                with patch.object(storage, "configuration", return_value=config), patch.object(storage, "disk_check") as checked, patch("sys.argv", ["maintenance", "disk-check"]):
                    storage.main()
                checked.assert_called_once()

    def test_alert_threshold_daily_limit_and_recovery(self):
        state = {}
        cases = [(94, 100, False), (95, 200, True), (99, 86400, False),
                 (95, 86600, True), (90, 86700, False), (89, 86800, False),
                 (95, 86900, True)]
        for usage, now, expected in cases:
            with self.subTest(usage=usage, now=now):
                self.assertEqual(storage.disk_alert_decision(state, usage, now), expected)

    def test_statistics_failure_still_sends_df_alert(self):
        with tempfile.TemporaryDirectory() as directory:
            config = dict(storage.DEFAULTS, HOST_ROOT=directory)
            with patch.object(storage, "run", return_value=b"Filesystem 1024-blocks Used Available Capacity Mounted on\n/dev/test 1000 950 50 95% /\n"), patch.object(storage, "cli", side_effect=RuntimeError("offline")), patch.object(storage, "notify") as notify:
                storage.disk_check(config)
            self.assertEqual(notify.call_count, 1)
            self.assertIn("95%", notify.call_args.args[2])
            self.assertIn("正文统计暂不可用", notify.call_args.args[2])

    def test_upload_download_checksum_failure_is_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            file = Path(directory)/"day.part-000001"
            file.write_bytes(b"complete encrypted volume")
            config = dict(storage.DEFAULTS)

            def commands(arguments, **kwargs):
                if arguments[1] == "download":
                    destination = Path(arguments[arguments.index("--saveto")+1])
                    (destination/file.name).write_bytes(b"damaged encrypted volume!")
                return b""

            with patch.object(storage, "run", side_effect=commands):
                with self.assertRaisesRegex(RuntimeError, "verification failed"):
                    storage.upload_verified(config, file, config["REMOTE_ROOT"]+"/"+file.name)
            self.assertTrue(file.exists(), "a failed proof must not discard the staged volume")

    def test_interrupted_import_resumes_and_extra_files_are_rejected_before_import(self):
        with tempfile.TemporaryDirectory() as directory:
            config = dict(storage.DEFAULTS, HOST_ROOT=directory)
            job_dir = Path(directory)/"imports"/"job-1"
            job_dir.mkdir(parents=True)
            volume = job_dir/"day.part-000001"
            volume.write_bytes(b"ciphertext")
            descriptor = job_dir/"day.volumes.json"
            item = {"name": volume.name, "bytes": volume.stat().st_size, "sha256": storage.file_digest(volume)}
            storage.write_json(descriptor, {"format": 1, "archive": {"digest": "a"*64}, "volumes": [item]})
            files = [item, {"name": descriptor.name, "bytes": descriptor.stat().st_size, "sha256": storage.file_digest(descriptor)}]
            job = {"id": "job-1", "status": "running", "files": files}
            storage.write_json(job_dir/"ready.json", job)
            storage.write_json(job_dir/"status.json", job)
            with patch.object(storage, "import_encrypted") as imported:
                storage.import_jobs(config)
            self.assertEqual(imported.call_count, 1)
            self.assertEqual(json.loads((job_dir/"status.json").read_text())["status"], "done")

            job_dir = Path(directory)/"imports"/"job-2"
            job_dir.mkdir()
            volume = job_dir/"day.part-000001"; volume.write_bytes(b"ciphertext")
            descriptor = job_dir/"day.volumes.json"
            storage.write_json(descriptor, {"format": 1, "archive": {"digest": "a"*64}, "volumes": [item]})
            extra = job_dir/"extra.age"; extra.write_bytes(b"unused")
            files = [{"name": file.name, "bytes": file.stat().st_size, "sha256": storage.file_digest(file)} for file in (volume, descriptor, extra)]
            storage.write_json(job_dir/"ready.json", {"id": "job-2", "status": "queued", "files": files})
            with patch.object(storage, "import_encrypted") as imported:
                storage.import_jobs(config)
            imported.assert_not_called()
            self.assertEqual(json.loads((job_dir/"status.json").read_text())["status"], "failed")

    def test_backup_patch_keeps_other_service_backup_and_is_idempotent(self):
        original = 'prefix\n  docker exec newapi-postgres pg_dump -U newapi -d newapi 2>/dev/null | gzip > "$STAGE/newapi-postgres.sql.gz"\nother service backup\nif [ ! -s "$STAGE/newapi-postgres.sql.gz" ] || [ ! -d "$STAGE/etc/caddy" ]; then\nsuffix\n'
        patched = installer.patch_backup(original)
        self.assertIn("other service backup", patched)
        self.assertIn("newapi-postgres.dump", patched)
        self.assertEqual(installer.patch_backup(patched), patched)
        with self.assertRaises(RuntimeError):
            installer.patch_backup("unreviewed script")

    def test_compose_patch_only_changes_app_import_environment(self):
        original = "services:\n  new-api:\n    image: ghcr.io/hhszzzz/new-api:main\n    environment:\n      EXISTING: preserved\n    labels:\n      label: preserved\n  new-api-updater:\n    image: updater\n"
        patched = installer.patch_compose(original)
        self.assertIn("      EXISTING: preserved", patched)
        self.assertTrue(patched.endswith("  new-api-updater:\n    image: updater\n"))
        self.assertEqual(installer.patch_compose(patched), patched)


if __name__ == "__main__":
    unittest.main()

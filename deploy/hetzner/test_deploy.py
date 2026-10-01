"""Root-only disposable deployment sandbox. All wrapper paths/commands are rewritten.
No production network or database access. Real OpenSSL verifies Go signatures.
"""
import hashlib
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
SIGNER = Path(sys.argv.pop(1)).resolve()
ASSETS = "sockt-windows-amd64.exe sockt-updater-windows-amd64.exe sockt-linux-amd64 sockt-linux-arm64 socktd-linux-amd64 socktd-linux-arm64 stable.json migration-plan release-meta install-sockt.ps1 install-sockt.sh sockt-windows-amd64.zip".split()
TAG = "v0.10.0"
COMMIT = "a" * 40

def call(*args, ok=True):
    p = subprocess.run([str(a) for a in args], capture_output=True, text=True, timeout=60)
    if ok and p.returncode:
        raise AssertionError(p.stdout + p.stderr)
    return p

class DeployTests(unittest.TestCase):
    def setUp(self):
        if os.geteuid() != 0:
            self.fail("Run this disposable sandbox as root (Linux CI)")
        self.tmp = tempfile.TemporaryDirectory(prefix="sockt-deploy-test-", dir="/var/lib")
        self.base = Path(self.tmp.name)
        self.base.chmod(0o755)
        self.addCleanup(self.tmp.cleanup)
        for name in ["root/releases", "base/incoming/" + TAG, "base/state", "base/backups", "web", "etc", "mock"]:
            (self.base / name).mkdir(parents=True, exist_ok=True)
        self.inc = self.base / "base/incoming" / TAG
        self.state = self.base / "base/state"
        self.current = self.base / "root/socktd"
        self.current.write_text("#!/bin/sh\nexit 0\n")
        self.current.chmod(0o755)
        self.legacy = self.current.read_bytes()
        keygen = call(SIGNER, "keygen", "-out", self.base / "seed")
        public = keygen.stdout.split(": ")[1].splitlines()[0]
        (self.base / "public.der").write_bytes(bytes.fromhex("302a300506032b6570032100" + public))
        call("openssl", "pkey", "-pubin", "-inform", "DER", "-in", self.base / "public.der", "-out", self.base / "etc/key.pem")
        for name in ASSETS:
            (self.inc / name).write_text("fixture " + name)
        (self.inc / "socktd-linux-amd64").write_text(f"#!/bin/sh\nprintf 'socktd {TAG} ({COMMIT})\\n'\n")
        (self.inc / "release-meta").write_text(f"{TAG}\n{COMMIT}\n{public}\n")
        (self.inc / "migration-plan").write_text("none\n")
        self.sha = hashlib.sha256((self.inc / "socktd-linux-amd64").read_bytes()).hexdigest()
        (self.base / "etc/migrate.env").write_text("DATABASE_URL=disposable-not-a-real-credential\n")
        (self.base / "etc/migrate.env").chmod(0o600)
        self.mock("runuser", '#!/bin/sh\nshift 3\nexec "$@"\n')
        self.mock("pm2", '#!/bin/sh\nexit 0\n')
        self.mock("sleep", '#!/bin/sh\nexit 0\n')
        self.mock("curl", f"""#!/bin/sh
if [ -f '{self.base}/fail-all' ]; then exit 1; fi
if [ -f '{self.base}/fail-new' ] && readlink '{self.current}' | grep -q 'socktd-v'; then exit 1; fi
echo ok
""")
        self.mock("systemd-run", f"""#!/bin/sh
if [ -f '{self.base}/interrupt' ]; then kill -KILL "$PPID"; exit 1; fi
if [ -f '{self.base}/migration-fail' ]; then exit 1; fi
exit 0
""")
        source = (ROOT / "deploy/hetzner/sockt-deploy").read_text()
        for old, new in {
            "ROOT=/opt/sockt": f"ROOT={self.base}/root",
            "BASE=/var/lib/sockt-deploy": f"BASE={self.base}/base",
            "WEB=/var/www/sockt-updates": f"WEB={self.base}/web",
            "PUBKEY=/etc/sockt/release-public-key.pem": f"PUBKEY={self.base}/etc/key.pem",
            "MIGRATION_ENV=/etc/sockt/socktd-migrate.env": f"MIGRATION_ENV={self.base}/etc/migrate.env",
            "PM2=/usr/bin/pm2": f"PM2={self.base}/mock/pm2",
            "/usr/sbin/runuser": f"{self.base}/mock/runuser",
            "export PATH=/usr/sbin:/usr/bin:/sbin:/bin": f"export PATH={self.base}/mock:/usr/sbin:/usr/bin:/sbin:/bin",
        }.items():
            self.assertIn(old, source, "sandbox path rewrite no longer matches implementation")
            source = source.replace(old, new)
        self.wrapper = self.base / "wrapper"
        self.wrapper.write_text(source)
        self.sign()

    def mock(self, name, content):
        p = self.base / "mock" / name
        p.write_text(content)
        p.chmod(0o755)

    def sign(self):
        call(SIGNER, "sign-checksums", "-key", self.base / "seed", "-version", TAG, "-dist", self.inc, "-out", self.inc / f"sockt-{TAG}-checksums.txt")

    def deploy(self, *extra, cmd="deploy"):
        args = ["bash", self.wrapper, cmd]
        if cmd in ("deploy", "publish"):
            args += ["--version", TAG, "--commit", COMMIT, "--sha256", self.sha]
        return call(*args, *extra, ok=False)

    def migration(self):
        (self.inc / "migration-plan").write_text("required\n")
        self.sign()
        receipt = self.base / "base/backups/test.verified"
        receipt.write_text(f"{TAG}\n{COMMIT}\n{self.sha}\n")
        receipt.chmod(0o600)
        return receipt

    def test_legacy_migration_and_rollback(self):
        p = self.deploy()
        self.assertEqual(p.returncode, 0, p.stderr)
        previous = Path((self.state / "previous").read_text().splitlines()[0])
        self.assertEqual(previous.read_bytes(), self.legacy)
        self.assertNotEqual(previous, self.current)
        self.assertEqual(self.deploy().returncode, 0)
        self.assertEqual(Path((self.state / "previous").read_text().splitlines()[0]), previous)
        self.assertEqual(self.deploy(cmd="publish").returncode, 0)
        self.assertTrue((self.base / "web/sockt-v0.10.0-sockt-linux-amd64").exists())
        self.assertEqual(self.deploy(cmd="rollback").returncode, 0)
        self.assertEqual(self.current.read_bytes(), self.legacy)
        self.assertNotEqual(self.deploy(cmd="publish").returncode, 0)

    def test_bad_signature_and_tampered_plan(self):
        (self.inc / "migration-plan").write_text("required")
        tampered = self.deploy()
        self.assertNotEqual(tampered.returncode, 0)
        self.assertIn("artifact checksum mismatch", tampered.stderr)
        self.assertFalse(self.current.is_symlink())
        self.sign()
        (self.inc / f"sockt-{TAG}-checksums.txt.sig").write_bytes(bytes(64))
        invalid = self.deploy()
        self.assertNotEqual(invalid.returncode, 0)
        self.assertIn("invalid checksum signature", invalid.stderr)

    def test_receipt_security(self):
        receipt = self.migration()
        denied = call("/usr/sbin/runuser", "-u", "nobody", "--", "touch", receipt, ok=False)
        self.assertNotEqual(denied.returncode, 0)
        forged = call("/usr/sbin/runuser", "-u", "nobody", "--", "touch", receipt.parent / "forged.verified", ok=False)
        self.assertNotEqual(forged.returncode, 0)
        receipt.parent.chmod(0o777)
        self.assertNotEqual(self.deploy("--allow-migrations", "--backup-id", "test").returncode, 0)
        receipt.parent.chmod(0o755)
        receipt.chmod(0o666)
        self.assertNotEqual(self.deploy("--allow-migrations", "--backup-id", "test").returncode, 0)
        receipt.chmod(0o600)
        receipt.write_text("forged receipt")
        self.assertNotEqual(self.deploy("--allow-migrations", "--backup-id", "test").returncode, 0)

    def test_migration_failure_and_interruption(self):
        self.migration()
        (self.base / "migration-fail").touch()
        self.assertNotEqual(self.deploy("--allow-migrations", "--backup-id", "test").returncode, 0)
        self.assertEqual(self.current.read_bytes(), self.legacy)
        self.assertTrue((self.state / "pending").exists())
        self.assertNotEqual(self.deploy().returncode, 0)
        self.assertEqual(self.deploy(cmd="rollback").returncode, 0)
        (self.base / "migration-fail").unlink()
        (self.base / "interrupt").touch()
        self.assertNotEqual(self.deploy("--allow-migrations", "--backup-id", "test").returncode, 0)
        self.assertTrue((self.state / "pending").exists())
        self.assertEqual(self.deploy(cmd="rollback").returncode, 0)

    def test_failed_health_and_failed_rollback(self):
        (self.base / "fail-new").touch()
        self.assertNotEqual(self.deploy().returncode, 0)
        self.assertEqual(self.current.read_bytes(), self.legacy)
        self.assertFalse((self.state / "pending").exists())
        (self.base / "fail-all").touch()
        self.assertNotEqual(self.deploy().returncode, 0)
        self.assertTrue((self.state / "pending").exists())
        self.assertNotEqual(self.deploy(cmd="rollback").returncode, 0)
        self.assertNotEqual(self.deploy(cmd="publish").returncode, 0)

    def test_rollback_activation_failure_is_not_reported_as_success(self):
        self.assertEqual(self.deploy().returncode, 0)
        self.mock("ln", "#!/bin/sh\nexit 1\n")
        failed = self.deploy(cmd="rollback")
        self.assertNotEqual(failed.returncode, 0)
        self.assertIn("could not activate previous executable", failed.stderr)
        self.assertTrue((self.state / "pending").exists())
        self.assertIn("socktd-v0.10.0", str(self.current.resolve()))

    def test_invalid_identity_and_missing_artifacts(self):
        for flag, value in [("--version", "vv0.10.0"), ("--commit", "abc123"),
                            ("--sha256", "not-a-hash"), ("--backup-id", "../forged")]:
            failed = self.deploy(flag, value)
            self.assertNotEqual(failed.returncode, 0)
            self.assertFalse(self.current.is_symlink())
        (self.inc / "sockt-updater-windows-amd64.exe").unlink()
        failed = self.deploy()
        self.assertIn("missing or symlinked artifact", failed.stderr)
        self.assertFalse((self.state / "pending").exists())

    def test_symlink_receipt_and_wrong_release_receipt(self):
        receipt = self.migration()
        content = receipt.read_bytes()
        receipt.unlink()
        other = self.base / "receipt-copy"
        other.write_bytes(content)
        other.chmod(0o600)
        receipt.symlink_to(other)
        self.assertNotEqual(self.deploy("--allow-migrations", "--backup-id", "test").returncode, 0)
        receipt.unlink()
        receipt.write_bytes(content.replace(b"v0.10.0", b"v0.11.0"))
        receipt.chmod(0o600)
        failed = self.deploy("--allow-migrations", "--backup-id", "test")
        self.assertIn("not bound to this release", failed.stderr)

    def test_systemd_reads_root_only_environment_before_drop(self):
        # Real local systemd; fake URL, no database connection or production file.
        envfile = self.base / "etc/migrate.env"
        self.assertNotEqual(call("/usr/sbin/runuser", "-u", "nobody", "--", "cat", envfile, ok=False).returncode, 0)
        result = call("systemd-run", "--quiet", "--wait", "--collect", "--pipe",
                      "--unit=sockt-env-test-" + self.base.name,
                      "-p", "User=nobody", "-p", "EnvironmentFile=" + str(envfile),
                      "-p", "NoNewPrivileges=yes", "-p", "ProtectSystem=strict",
                      "/bin/sh", "-c", 'test -n "$DATABASE_URL"', ok=False)
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_dry_run_and_symlink(self):
        self.assertEqual(self.deploy("--dry-run").returncode, 0)
        self.assertFalse(self.current.is_symlink())
        self.assertFalse((self.state / "previous").exists())
        asset = self.inc / "socktd-linux-amd64"
        asset.unlink()
        asset.symlink_to(self.current)
        self.assertNotEqual(self.deploy().returncode, 0)

if __name__ == "__main__":
    unittest.main(verbosity=2)

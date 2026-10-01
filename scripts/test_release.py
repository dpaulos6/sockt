"""Offline regression tests: disposable keys/artifacts; no production endpoints."""
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
ASSETS = "sockt-windows-amd64.exe sockt-updater-windows-amd64.exe sockt-linux-amd64 sockt-linux-arm64 socktd-linux-amd64 socktd-linux-arm64 stable.json migration-plan release-meta install-sockt.ps1 install-sockt.sh sockt-windows-amd64.zip".split()

def run(*args, ok=True, env=None, cwd=ROOT):
    p = subprocess.run([str(a) for a in args], cwd=cwd, env=env, text=True, capture_output=True, timeout=240)
    if ok and p.returncode:
        raise AssertionError(f"{args[0]} failed ({p.returncode}): {p.stdout} {p.stderr}")
    return p

class ReleaseTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.tmp = tempfile.TemporaryDirectory(prefix="sockt-release-test-")
        cls.addClassCleanup(cls.tmp.cleanup)
        cls.work = Path(cls.tmp.name)
        cls.signer = cls.work / ("signer.exe" if os.name == "nt" else "signer")
        run("go", "build", "-o", cls.signer, "./cmd/sockt-release")
        cls.openssl = shutil.which("openssl")
        cls.bash = shutil.which("bash")
        if os.name == "nt":
            cls.openssl = cls.openssl or "C:/Program Files/Git/usr/bin/openssl.exe"
            cls.bash = "C:/Program Files/Git/bin/bash.exe"
        result = run(cls.signer, "keygen", "-out", cls.work / "seed")
        cls.public = re.search(r"PUBLIC key .*: ([0-9a-f]{64})", result.stdout)[1]
        (cls.work / "public.hex").write_text(cls.public)
        (cls.work / "public.der").write_bytes(bytes.fromhex("302a300506032b6570032100" + cls.public))
        for name in ASSETS:
            (cls.work / name).write_bytes(("fixture " + name).encode())
        run(cls.signer, "sign", "-key", cls.work / "seed", "-version", "v0.10.0", "-dist", cls.work, "-out", cls.work / "stable.json")
        cls.payload = cls.work / "sockt-v0.10.0-checksums.txt"
        run(cls.signer, "sign-checksums", "-key", cls.work / "seed", "-version", "v0.10.0", "-dist", cls.work, "-out", cls.payload)
        (cls.work / "tampered").write_bytes(cls.payload.read_bytes() + b"tampered")
        (cls.work / "bad.sig").write_bytes(bytes(64))

    def test_real_go_openssl_signatures(self):
        signature = Path(str(self.payload) + ".sig")
        self.assertEqual(len(signature.read_bytes()), 64)
        for payload, sig, good in [(self.payload, signature, True), (self.work / "tampered", signature, False), (self.payload, self.work / "bad.sig", False)]:
            p = run(self.openssl, "pkeyutl", "-verify", "-pubin", "-inkey", self.work / "public.der", "-keyform", "DER", "-rawin", "-in", payload, "-sigfile", sig, ok=False)
            self.assertEqual(p.returncode == 0, good, p.stderr)
        run(self.signer, "check-key", "-key", self.work / "seed", "-public-key", self.public)
        self.assertNotEqual(run(self.signer, "check-key", "-key", self.work / "seed", "-public-key", "00" * 32, ok=False).returncode, 0)

    def linux_installer_fixture(self):
        # Fake transport only: the actual installer/OpenSSL/hash/install paths run.
        mock = self.work / "mock"
        mock.mkdir(exist_ok=True)
        curl = mock / "curl"
        curl.write_text("#!/bin/bash\nset -eu\nurl= out=\nwhile (($#)); do case \"$1\" in https://*) url=$1; shift;; -o) out=$2; shift 2;; *) shift;; esac; done\n[[ $url == https://chat.dpaulos.pt/updates/releases/v0.10.0/* || $url == https://github.com/dpaulos6/sockt/releases/download/v0.10.0/* ]] || exit 9\ncp \"$SOCKT_TEST_FIXTURE/${url##*/}\" \"$out\"\n", encoding="utf-8", newline="\n")
        curl.chmod(0o755)
        installer = self.work / "installer.sh"
        installer.write_text((ROOT / "scripts/install.sh").read_text().replace("__SOCKT_VERSION__", "v0.10.0").replace("__SOCKT_PUBLIC_KEY__", self.public), encoding="utf-8", newline="\n")
        bindir = self.work / "linux-bin"
        env = dict(os.environ, SOCKT_TEST_FIXTURE=self.work.as_posix(), XDG_BIN_HOME=bindir.as_posix(), SOCKT_REPOSITORY="", SOCKT_INSTALL_ACTION="install")
        # Source preserves MSYS's translated PATH when tests run under Git Bash.
        env["SOCKT_TEST_MOCK"] = mock.as_posix()
        command = 'mock_path=$SOCKT_TEST_MOCK; if command -v cygpath >/dev/null; then mock_path=$(cygpath -u "$mock_path"); fi; export PATH="$mock_path:$PATH"; test "$(command -v curl)" = "$mock_path/curl" || exit 99; "$BASH" "$1" "$2"'
        return installer, bindir, env, command

    def test_linux_installer_real_verification(self):
        installer, bindir, env, command = self.linux_installer_fixture()
        for repository in ["", "dpaulos6/sockt"]:
            for version in ["0.10.0", "v0.10.0"]:
                run(self.bash, "-c", command, "test", installer.as_posix(), version, env=dict(env, SOCKT_REPOSITORY=repository))
        self.assertEqual((bindir / "sockt").read_bytes(), (self.work / "sockt-linux-amd64").read_bytes())
        artifact = self.work / "sockt-linux-amd64"
        original = artifact.read_bytes()
        try:
            artifact.write_bytes(b"tampered")
            self.assertNotEqual(run(self.bash, "-c", command, "test", installer.as_posix(), "v0.10.0", env=env, ok=False).returncode, 0)
            self.assertEqual((bindir / "sockt").read_bytes(), original)
        finally:
            artifact.write_bytes(original)

    def test_linux_installer_rejects_bad_signature(self):
        installer, bindir, env, command = self.linux_installer_fixture()
        bindir.mkdir(exist_ok=True)
        (bindir / "sockt").write_bytes(b"existing verified client")
        signature = Path(str(self.payload) + ".sig")
        original = signature.read_bytes()
        installed = (self.work / "linux-bin/sockt").read_bytes()
        try:
            for invalid in [bytes(64), original.hex().encode()]:
                signature.write_bytes(invalid)
                p = run(self.bash, "-c", command, "test", installer.as_posix(), "v0.10.0", env=env, ok=False)
                self.assertNotEqual(p.returncode, 0)
                self.assertEqual((self.work / "linux-bin/sockt").read_bytes(), installed)
        finally:
            signature.write_bytes(original)

    def test_linker_embeds_key_and_normalizes_version(self):
        env = dict(os.environ, SOCKT_TEST_PUBLIC_KEY=self.public, SOCKT_TEST_MANIFEST=str(self.work / "stable.json"))
        flags = "-X sockt/internal/updater.PublicKeyHex=" + self.public + " -X sockt/internal/buildinfo.Version=v0.10.0"
        run("go", "test", "./internal/updater", "-run", "^TestReleaseInjectedValues$", "-count=1", "-ldflags=" + flags, env=env)

    def test_version_urls(self):
        manifest = json.loads((self.work / "stable.json").read_text())
        self.assertEqual(manifest["version"], "0.10.0")
        for asset in manifest["artifacts"].values():
            self.assertIn("/sockt-v0.10.0-", asset["url"])
            self.assertNotIn("vv", asset["url"])

    def test_signer_version_rejection_and_normalization(self):
        for version in ["0.10.0", "v0.10.0"]:
            output = self.work / "version-check.json"
            run(self.signer, "sign", "-key", self.work / "seed", "-version", version,
                "-dist", self.work, "-out", output)
            self.assertEqual(json.loads(output.read_text())["version"], "0.10.0")
        for version in ["vv0.10.0", "v01.2.3", "0.10.0;echo bad", "v1.2.3\n"]:
            for command in ["sign", "sign-checksums"]:
                p = run(self.signer, command, "-key", self.work / "seed", "-version", version,
                        "-dist", self.work, "-out", self.work / "invalid-version", ok=False)
                self.assertNotEqual(p.returncode, 0)
                self.assertFalse((self.work / "invalid-version").exists())

    def test_workflow_inputs(self):
        for tag in ["v0.10.0", "v1.2.3; touch INJECTED", "v1.2.3$(touch INJECTED)", "vv1.2.3", "v01.2.3", "v1.2.3\nboom"]:
            env = dict(os.environ, RELEASE_TAG=tag, APPLY_MIGRATIONS="false", BACKUP_ID="")
            p = run(self.bash, "-c", "source scripts/validate-release.sh; validate_release", env=env, ok=False)
            self.assertEqual(p.returncode == 0, tag == "v0.10.0")
        for backup in ["good-123", "x'; touch INJECTED; #", "../receipt", "x\nfoo"]:
            env = dict(os.environ, RELEASE_TAG="v0.10.0", APPLY_MIGRATIONS="true", BACKUP_ID=backup)
            p = run(self.bash, "-c", "source scripts/validate-release.sh; validate_release", env=env, ok=False)
            self.assertEqual(p.returncode == 0, backup == "good-123")
        self.assertFalse((ROOT / "INJECTED").exists())
        # A regression guard for expression interpolation in workflow run blocks.
        text = (ROOT / ".github/workflows/release.yml").read_text()
        in_run = False
        for line in text.splitlines():
            if re.match(r"      - (?:name:|uses:)", line) or re.match(r"        (?:env|with|if):", line):
                in_run = False
            if re.match(r"(?:      - |        )run:", line):
                in_run = True
            if in_run:
                self.assertNotIn("${{", line)

    def test_commit_and_migration_inputs(self):
        base = dict(os.environ, RELEASE_TAG="v0.10.0", RELEASE_COMMIT="a" * 40,
                    APPLY_MIGRATIONS="false", BACKUP_ID="")
        run(self.bash, "scripts/validate-release.sh", env=base)
        for commit in ["", "abcdef0", "A" * 40, "a" * 41, "a" * 39 + ";", "$(touch INJECTED)", "a" * 40 + "\n"]:
            with self.subTest(commit=commit):
                p = run(self.bash, "scripts/validate-release.sh", env=dict(base, RELEASE_COMMIT=commit), ok=False)
                self.assertEqual(p.returncode, 2, p.stderr)
                self.assertIn("commit SHA required", p.stderr)
        for approval, backup, good in [("true", "receipt-1", True), ("true", "", False),
                                      ("false", "receipt-1", False), ("TRUE", "", False),
                                      ("", "", False), ("true; false", "r", False)]:
            with self.subTest(approval=approval, backup=backup):
                p = run(self.bash, "scripts/validate-release.sh", env=dict(base, APPLY_MIGRATIONS=approval, BACKUP_ID=backup), ok=False)
                self.assertEqual(p.returncode == 0, good, p.stderr)

    def test_key_and_remote_validation(self):
        base = dict(os.environ, SOCKT_UPDATE_PUBLIC_KEY=self.public,
                    DEPLOY_USER="socktdeploy", DEPLOY_HOST="staging.invalid")
        command = "source scripts/validate-release.sh; validate_key; validate_remote"
        run(self.bash, "-c", command, env=base)
        for updates in [{"SOCKT_UPDATE_PUBLIC_KEY": ""}, {"SOCKT_UPDATE_PUBLIC_KEY": "00" * 32},
                        {"DEPLOY_USER": "-oProxyCommand=bad"}, {"DEPLOY_HOST": "host; touch INJECTED"},
                        {"DEPLOY_USER": "name\nother"}, {"DEPLOY_HOST": "$(touch INJECTED)"}]:
            p = run(self.bash, "-c", command, env=dict(base, **updates), ok=False)
            self.assertEqual(p.returncode, 2, p.stderr)

    def test_workflow_local_references(self):
        # Additional regression guard; cryptographic tests above exercise behavior.
        references = set()
        for workflow in (ROOT / ".github/workflows").glob("*.yml"):
            for path in re.findall(r"(?<![A-Za-z0-9_./-])(?:\./)?((?:scripts|deploy|cmd|internal)/[A-Za-z0-9_./*-]+)", workflow.read_text()):
                path = path.removesuffix("/...")
                references.add(path)
                self.assertTrue(list(ROOT.glob(path)), f"{workflow.name} references missing {path}")
        self.assertIn("scripts/test_release.py", references)
        self.assertIn("scripts/validate-release.sh", references)
        for helper in ["scripts/test-installer.ps1", "deploy/hetzner/test_deploy.py",
                       "internal/updater/release_build_test.go"]:
            self.assertTrue((ROOT / helper).is_file(), f"required test helper missing: {helper}")
        # The compiler reports no error for -run patterns matching zero tests.
        listed = run("go", "test", "./internal/updater", "-list", "^TestReleaseInjectedValues$")
        self.assertIn("TestReleaseInjectedValues", listed.stdout)

    def test_build_rejects_bad_keys(self):
        for key in ["", "zz" * 32, "00" * 32, "a" * 63, "a" * 64 + " -X bad=1"]:
            env = dict(os.environ, SOCKT_RELEASE_VERSION="v0.10.0", SOCKT_UPDATE_PUBLIC_KEY=key, SOCKT_RELEASE_COMMIT="a" * 40)
            self.assertNotEqual(run(self.bash, "scripts/build.sh", env=env, ok=False).returncode, 0)

    @unittest.skipUnless(os.name == "nt", "Windows installer runtime checks run on Windows CI")
    def test_windows_powershell_versions(self):
        env = dict(os.environ)
        env["PATH"] = str(Path(self.openssl).parent) + os.pathsep + env["PATH"]
        for executable, major in [("powershell.exe", 5), ("pwsh.exe", 7)]:
            p = run(executable, "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", ROOT / "scripts/test-installer.ps1", "-Fixture", self.work, "-ExpectedMajor", major, env=env)
            print(p.stdout.strip())

if __name__ == "__main__":
    unittest.main(verbosity=2)

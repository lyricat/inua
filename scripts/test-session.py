"""Exercise session installation and startup without root or a compositor."""

import configparser
import os
from pathlib import Path
import subprocess
import tempfile
import tomllib
import unittest


ROOT = Path(__file__).resolve().parent.parent


class SessionTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix="inua-session-")
        self.addCleanup(self.tmp.cleanup)
        self.home = Path(self.tmp.name) / "home with spaces"
        self.bin = self.home / ".local/bin"
        self.bin.mkdir(parents=True)
        self.env = dict(os.environ, HOME=str(self.home), PATH="/usr/bin:/bin")
        self.env.pop("XDG_CONFIG_HOME", None)
        self.config = self.home / ".config/hypr/hyprland.conf"
        self.config.parent.mkdir(parents=True)
        self.config.write_text("# test\n")
        self.executable("inua-shell", "#!/bin/sh\nexit 0\n")
        # inua records how it was called; `inua config hyprland` passes.
        self.calls = self.home / "inua-calls"
        self.executable("inua", f'#!/bin/sh\necho "$*" >> "{self.calls}"\nexit 0\n')
        # Never the user's systemd: systemctl records how it was called.
        self.systemctl = self.home / "systemctl-calls"
        self.executable("systemctl", f'#!/bin/sh\necho "$*" >> "{self.systemctl}"\nexit 0\n')
        self.executable("start-hyprland", '#!/bin/sh\nprintf "argc=%s\\nXDG_CONFIG_HOME=%s\\n" "$#" "${XDG_CONFIG_HOME-}"\nexit 17\n')

    def executable(self, name, content):
        path = self.bin / name
        path.write_text(content)
        path.chmod(0o755)

    def launch(self):
        return subprocess.run(
            ["sh", str(ROOT / "scripts/inua-session")], env=self.env,
            text=True, capture_output=True,
        )

    def test_default_config_and_exit_status(self):
        result = self.launch()
        self.assertEqual(result.returncode, 17, result.stderr)
        self.assertEqual(result.stdout.splitlines(), ["argc=0", "XDG_CONFIG_HOME="])

    def test_xdg_config_home(self):
        xdg = self.home / "other config"
        config = xdg / "hypr/hyprland.conf"
        config.parent.mkdir(parents=True)
        config.write_text("# custom\n")
        self.env["XDG_CONFIG_HOME"] = str(xdg)
        result = self.launch()
        self.assertEqual(result.returncode, 17, result.stderr)
        self.assertEqual(result.stdout.splitlines(), ["argc=0", f"XDG_CONFIG_HOME={xdg}"])

    def test_missing_config_is_left_to_hyprland(self):
        self.config.unlink()
        result = self.launch()
        self.assertEqual(result.returncode, 17, result.stderr)
        self.assertEqual(result.stdout.splitlines(), ["argc=0", "XDG_CONFIG_HOME="])
        self.assertFalse(self.config.exists())

    def test_config_is_checked_for_the_shell(self):
        result = self.launch()
        self.assertEqual(result.returncode, 17, result.stderr)
        self.assertEqual(self.calls.read_text().splitlines(), [f"config hyprland {self.config}", "config apply", "portals dir"])

    def test_portal_directory_given_to_frontend(self):
        portals = self.home / ".local/share/inua/portals"
        self.executable("inua", f'#!/bin/sh\n[ "$1" = portals ] && echo "{portals}"\nexit 0\n')
        result = self.launch()
        self.assertEqual(result.returncode, 17, result.stderr)
        self.assertEqual(self.systemctl.read_text().splitlines(), [
            f"--user set-environment XDG_DESKTOP_PORTAL_DIR={portals}",
            "--user try-restart xdg-desktop-portal.service",
        ])

    def test_no_portal_directory_takes_it_away(self):
        self.executable("inua", '#!/bin/sh\n[ "$1" = portals ] && exit 1\nexit 0\n')
        result = self.launch()
        self.assertEqual(result.returncode, 17, result.stderr)
        self.assertEqual(self.systemctl.read_text().splitlines(), [
            "--user unset-environment XDG_DESKTOP_PORTAL_DIR",
            "--user try-restart xdg-desktop-portal.service",
        ])

    def test_config_without_shell_does_not_start_compositor(self):
        self.executable("inua", '#!/bin/sh\necho "hyprland.conf does not start inua-shell"\nexit 1\n')
        result = self.launch()
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(result.stdout, "")
        self.assertIn("does not start inua-shell", result.stderr)
        self.assertIn("Hyprland session is unaffected", result.stderr)

    def test_virtual_screen_is_awaited_for_this_hyprland(self):
        # inua-remote records its arguments and the process it belongs to,
        # which is the one that becomes the compositor.
        remote = self.home / "inua-remote-calls"
        self.executable("inua-remote", f'#!/bin/sh\necho "$* $PPID" > "{remote}.tmp"\nmv "{remote}.tmp" "{remote}"\n')
        self.executable("start-hyprland", '#!/bin/sh\necho "pid=$$"\nwhile [ ! -e "$HOME/inua-remote-calls" ]; do sleep 0.05; done\nexit 17\n')
        result = self.launch()
        self.assertEqual(result.returncode, 17, result.stderr)
        pid = result.stdout.split("=")[1].strip()
        self.assertEqual(remote.read_text().split(), ["--await-session", pid])

    def test_missing_shell_does_not_start_compositor(self):
        (self.bin / "inua-shell").unlink()
        result = self.launch()
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(result.stdout, "")
        self.assertIn("inua-shell", result.stderr)

    def test_older_hyprland_without_start_hyprland(self):
        # Only the fakes on PATH: not this machine's start-hyprland.
        (self.bin / "start-hyprland").unlink()
        self.executable("Hyprland", '#!/bin/sh\necho plain\nexit 18\n')
        self.env["PATH"] = str(self.bin)
        launch = lambda: subprocess.run(
            ["/bin/sh", str(ROOT / "scripts/inua-session")], env=self.env,
            text=True, capture_output=True,
        )
        result = launch()
        self.assertEqual(result.returncode, 18, result.stderr)
        self.assertEqual(result.stdout.splitlines(), ["plain"])
        (self.bin / "Hyprland").unlink()
        result = launch()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Hyprland is not on PATH", result.stderr)

    def test_niri_session(self):
        # inua-session niri runs niri as a session; the Hyprland config is
        # not checked, inua-remote is not started.
        self.executable("niri", '#!/bin/sh\nprintf "niri %s\\n" "$*"\nexit 19\n')
        self.executable("inua-remote", f'#!/bin/sh\ntouch "{self.home}/inua-remote-calls"\nexit 0\n')
        self.config.write_text("# a Hyprland config without the shell\n")
        result = subprocess.run(["sh", str(ROOT / "scripts/inua-session"), "niri"], env=self.env, text=True, capture_output=True)
        self.assertEqual(result.returncode, 19, result.stderr)
        self.assertEqual(result.stdout.splitlines(), ["niri --session"])
        self.assertFalse((self.home / "inua-remote-calls").exists())
        # A niri config without the shell is refused, as Hyprland's is.
        kdl = self.home / ".config/niri/config.kdl"
        kdl.parent.mkdir(parents=True)
        kdl.write_text('binds { Mod+Q { close-window; } }\n')
        result = subprocess.run(["sh", str(ROOT / "scripts/inua-session"), "niri"], env=self.env, text=True, capture_output=True)
        self.assertEqual(result.returncode, 1)
        self.assertIn("niri session is unaffected", result.stderr)
        kdl.write_text('spawn-at-startup "inua-shell"\n')
        result = subprocess.run(["sh", str(ROOT / "scripts/inua-session"), "niri"], env=self.env, text=True, capture_output=True)
        self.assertEqual(result.returncode, 19, result.stderr)
        # Without niri, or with an unknown compositor, nothing starts.
        (self.bin / "niri").unlink()
        result = subprocess.run(["sh", str(ROOT / "scripts/inua-session"), "niri"], env=self.env, text=True, capture_output=True)
        self.assertEqual(result.returncode, 1)
        self.assertIn("niri is not on PATH", result.stderr)
        result = subprocess.run(["sh", str(ROOT / "scripts/inua-session"), "sway"], env=self.env, text=True, capture_output=True)
        self.assertEqual(result.returncode, 2)

    def test_install_and_reinstall_preserve_other_sessions(self):
        dest = Path(self.tmp.name) / "staged root"
        sessions = dest / "usr/share/wayland-sessions"
        sessions.mkdir(parents=True)
        hypr = sessions / "hyprland.desktop"
        hypr.write_text("existing session\n")
        for _ in range(2):
            result = subprocess.run(
                ["sh", str(ROOT / "scripts/install-session.sh"), "gdm", str(dest)],
                cwd=self.tmp.name, text=True, capture_output=True,
            )
            self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(hypr.read_text(), "existing session\n")
        niri = configparser.ConfigParser()
        niri.read(sessions / "inua-niri.desktop")
        self.assertEqual(niri["Desktop Entry"]["Exec"], "/usr/local/bin/inua-session niri")
        self.assertEqual(niri["Desktop Entry"]["TryExec"], "niri")
        desktop = configparser.ConfigParser()
        desktop.read(sessions / "inua.desktop")
        entry = desktop["Desktop Entry"]
        self.assertEqual(entry["Name"], "Inua")
        self.assertEqual(entry["Type"], "Application")
        self.assertIn("Hyprland", entry["DesktopNames"].split(";"))
        launcher = dest / entry["Exec"].lstrip("/")
        self.assertEqual(entry["TryExec"], entry["Exec"])
        self.assertTrue(os.access(launcher, os.X_OK))
        result = subprocess.run([str(launcher)], env=self.env, text=True, capture_output=True)
        self.assertEqual(result.returncode, 17, result.stderr)
        self.assertEqual(result.stdout.splitlines(), ["argc=0", "XDG_CONFIG_HOME="])

    def test_greetd_configuration_replaces_and_backs_up_existing_settings(self):
        dest = Path(self.tmp.name) / "greetd root"
        for number, existing in enumerate((None, "# first login settings\n", "# second login settings\n")):
            config = dest / "etc/greetd/config.toml"
            if existing is not None:
                config.write_text(existing)
            result = subprocess.run(
                ["sh", str(ROOT / "scripts/install-session.sh"), "greetd", str(dest)],
                text=True, capture_output=True,
            )
            self.assertEqual(result.returncode, 0, result.stderr)
            if existing is not None:
                self.assertEqual(config.with_name(f"config.toml.~{number}~").read_text(), existing)
            data = tomllib.loads(config.read_text())
            self.assertEqual(data["default_session"]["user"], "_greetd")
            command = data["default_session"]["command"]
            self.assertIn("tuigreet", command)
            self.assertIn("--sessions /usr/share/wayland-sessions", command)
            self.assertIn("--cmd /usr/local/bin/inua-session", command)
            self.assertNotIn("initial_session", data)
            # An unchanged reinstall must not add backups or replace earlier ones.
            result = subprocess.run(
                ["sh", str(ROOT / "scripts/install-session.sh"), "greetd", str(dest)],
                text=True, capture_output=True,
            )
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(len(list(config.parent.glob("config.toml.~*~"))), number)
        self.assertEqual(config.with_name("config.toml.~1~").read_text(), "# first login settings\n")
        self.assertFalse(config.with_name("config.toml.inua").exists())

    def test_greetd_greeter_account_and_vt_follow_the_package(self):
        cases = [
            # Arch: the package's config names greeter and VT 1.
            ("arch", "greeter:x:964:964::/:/usr/bin/nologin\n",
             '[terminal]\nvt = 1\n[default_session]\ncommand = "agreety --cmd /bin/sh"\nuser = "greeter"\n', "greeter", 1),
            # Fedora: the greetd account, no config yet.
            ("fedora", "greetd:x:975:975::/:/sbin/nologin\n", None, "greetd", 7),
            # An earlier config of ours naming _greetd where there is none.
            ("stale", "greeter:x:964:964::/:/usr/bin/nologin\n",
             '[terminal]\nvt = 7\n[default_session]\ncommand = "tuigreet"\nuser = "_greetd"\n', "greeter", 7),
        ]
        for name, passwd, existing, user, vt in cases:
            with self.subTest(name):
                dest = Path(self.tmp.name) / name
                (dest / "etc/greetd").mkdir(parents=True)
                (dest / "etc/passwd").write_text(passwd)
                config = dest / "etc/greetd/config.toml"
                if existing is not None:
                    config.write_text(existing)
                result = subprocess.run(
                    ["sh", str(ROOT / "scripts/install-session.sh"), "greetd", str(dest)],
                    text=True, capture_output=True,
                )
                self.assertEqual(result.returncode, 0, result.stderr)
                data = tomllib.loads(config.read_text())
                self.assertEqual(data["default_session"]["user"], user)
                self.assertEqual(data["terminal"]["vt"], vt)
                self.assertIn("--cmd /usr/local/bin/inua-session", data["default_session"]["command"])

    def test_gdm_and_sddm_do_not_install_greetd_configuration(self):
        for dm in ("gdm", "sddm"):
            dest = Path(self.tmp.name) / dm
            result = subprocess.run(
                ["sh", str(ROOT / "scripts/install-session.sh"), dm, str(dest)],
                text=True, capture_output=True,
            )
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertTrue((dest / "usr/share/wayland-sessions/inua.desktop").is_file())
            self.assertFalse((dest / "etc/greetd").exists())

    def test_invalid_dm_fails_before_installation(self):
        dest = Path(self.tmp.name) / "invalid"
        result = subprocess.run(
            ["sh", str(ROOT / "scripts/install-session.sh"), "unknown", str(dest)],
            text=True, capture_output=True,
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(dest.exists())


if __name__ == "__main__":
    unittest.main()

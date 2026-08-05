#!/bin/sh
# Install rode-dsp on Linux. Run with sudo.
#
#   sudo ./install.sh           binary + udev rule (interactive use)
#   sudo ./install.sh --boot    the above, plus apply your settings automatically
#                               at boot, on hotplug and after resume
#   sudo ./install.sh --uninstall
#
# Re-running is safe. No config file is installed or copied: the service reads
# the config in your home directory, so changing a setting takes effect at the
# next boot with nothing to re-publish. Until you change a setting there is no
# config at all, and the microphone simply uses its own defaults.
#
# Paths follow the FHS split between local software and distro packages: a
# hand-built binary belongs in /usr/local/bin, and admin-installed udev rules
# and units belong in /etc, where they take priority over anything a package
# ships in /usr/lib. Override either from the environment:
#
#   sudo BIN_DIR=/opt/bin ./install.sh --boot

set -eu

BIN_DIR=${BIN_DIR:-/usr/local/bin}
UDEV_DIR=${UDEV_DIR:-/etc/udev/rules.d}
UNIT_DIR=${UNIT_DIR:-/etc/systemd/system}

SRC=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
REPO=$(CDPATH='' cd -- "$SRC/../.." && pwd)

die() { echo "install.sh: $*" >&2; exit 1; }

need() { command -v "$1" >/dev/null || die "$1 not found -- $2"; }

[ "$(id -u)" -eq 0 ] || die "must be run as root (use sudo)"

reload_udev() {
	udevadm control --reload-rules
	udevadm trigger --subsystem-match=hidraw
}

# The invoking user's config, not root's, which is why this uses SUDO_USER
# rather than $HOME. Matches DefaultConfigPath in internal/dsp/paths.go, so the
# service reads the same file the tool writes. sudo usually scrubs
# XDG_CONFIG_HOME, so ~/.config is the normal answer.
user_config() {
	[ -n "${SUDO_USER:-}" ] || return 1
	home=$(getent passwd "$SUDO_USER" | cut -d: -f6)
	echo "${XDG_CONFIG_HOME:-$home/.config}/rode-dsp/config.json"
}

# The unit has to name the binary this script installed and the config file of
# the user installing it, so it is generated rather than copied verbatim.
install_unit() {
	install -d "$UNIT_DIR"
	sed -e "s|/usr/local/bin/rode-dsp|$BIN_DIR/rode-dsp|" \
	    -e "s|@CONFIG@|$config|g" \
	    "$SRC/rode-dsp.service" >"$UNIT_DIR/rode-dsp.service"
	chmod 644 "$UNIT_DIR/rode-dsp.service"
}

uninstall() {
	if command -v systemctl >/dev/null; then
		# Nothing to "disable": the unit has no [Install] section, udev starts it.
		systemctl stop rode-dsp.service 2>/dev/null || true
	fi
	rm -f "$UNIT_DIR/rode-dsp.service" \
	      "$UDEV_DIR/70-rode-nt-usb-mini.rules" \
	      "$UDEV_DIR/71-rode-dsp-service.rules" \
	      "$BIN_DIR/rode-dsp"
	command -v systemctl >/dev/null && systemctl daemon-reload
	command -v udevadm >/dev/null && reload_udev
	echo "Removed. Your settings were left alone."
}

case "${1:-}" in
--uninstall) uninstall; exit 0 ;;
--boot|"") ;;
*) die "unknown option: $1" ;;
esac

# Everything is checked before anything is written, so a failure here cannot
# leave a half-installed system.
need udevadm "this installer needs udev"
[ -x "$REPO/rode-dsp" ] || die "no rode-dsp binary in $REPO -- run 'go build' first"

if [ "${1:-}" = "--boot" ]; then
	# The service unit is systemd; see the readme for the group-based
	# alternative on OpenRC/runit distros.
	need systemctl "--boot needs systemd"
	# The config does not have to exist yet. The unit skips itself until it
	# does, so installing before setting anything up is fine.
	config=$(user_config) || die "cannot tell whose config the service should read; run this with sudo, not as root directly"
fi

install -Dm755 "$REPO/rode-dsp" "$BIN_DIR/rode-dsp"
install -Dm644 "$SRC/70-rode-nt-usb-mini.rules" "$UDEV_DIR/70-rode-nt-usb-mini.rules"
echo "Installed $BIN_DIR/rode-dsp and the uaccess udev rule."

if [ "${1:-}" = "--boot" ]; then
	install_unit
	install -Dm644 "$SRC/71-rode-dsp-service.rules" "$UDEV_DIR/71-rode-dsp-service.rules"
	systemctl daemon-reload
	echo "Installed rode-dsp.service and its udev trigger, reading $config."
	[ -f "$config" ] || echo "That file does not exist yet, so the service will skip until you set a value."
fi

# Applies the new rules to the microphone if it is already plugged in, which
# with --boot also starts the service straight away.
reload_udev
echo "Done. Run 'rode-dsp status' to check; replug the microphone if access still fails."

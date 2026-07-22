#!/bin/bash
set -Eeuo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
ROOT=$(mktemp -d)
trap 'rm -rf -- "$ROOT"' EXIT

export KVMCTL_CONFIGFS_ROOT="$ROOT/configfs"
export KVMCTL_UDC_CLASS="$ROOT/udc"
export KVMCTL_DEV_ROOT="$ROOT/dev"
export KVMCTL_GADGET_NAME="test-kvmctl"
export KVMCTL_UDC_WAIT_SECONDS=2
export TEST_UDC="musb-hdrc.test"

FAKEBIN="$ROOT/fakebin"
mkdir -p "$FAKEBIN" "$KVMCTL_CONFIGFS_ROOT/usb_gadget" "$KVMCTL_UDC_CLASS" "$KVMCTL_DEV_ROOT"

cat > "$FAKEBIN/id" <<'EOF'
#!/bin/sh
if [ "${1:-}" = "-u" ]; then
    echo 0
    exit 0
fi
exec /usr/bin/id "$@"
EOF

for command in modprobe mountpoint chgrp chmod; do
    cat > "$FAKEBIN/$command" <<'EOF'
#!/bin/sh
exit 0
EOF
done

cat > "$FAKEBIN/sleep" <<'EOF'
#!/bin/sh
if [ "${TEST_CREATE_UDC_ON_SLEEP:-0}" = "1" ]; then
    : > "$KVMCTL_UDC_CLASS/$TEST_UDC"
    : > "$KVMCTL_DEV_ROOT/hidg0"
    : > "$KVMCTL_DEV_ROOT/hidg1"
    : > "$KVMCTL_DEV_ROOT/hidg2"
fi
exit 0
EOF

cat > "$FAKEBIN/ln" <<'EOF'
#!/bin/sh
if [ "${KVMCTL_FAIL_LN:-0}" = "1" ]; then
    exit 1
fi
source=$2
destination=$3
if [ -d "$destination" ]; then
    destination="$destination/$(basename "$source")"
fi
: > "$destination"
EOF

cat > "$FAKEBIN/mkdir" <<'EOF'
#!/bin/sh
for path in "$@"; do
    /usr/bin/mkdir "$path"
    if [ "$path" = "$KVMCTL_CONFIGFS_ROOT/usb_gadget/$KVMCTL_GADGET_NAME" ]; then
        /usr/bin/mkdir -p "$path/strings" "$path/configs" "$path/functions"
    fi
    case "$path" in
        */configs/c.1) /usr/bin/mkdir -p "$path/strings" ;;
    esac
done
EOF

cat > "$FAKEBIN/rmdir" <<'EOF'
#!/bin/sh
for path in "$@"; do
    case "$path" in
        "$KVMCTL_CONFIGFS_ROOT"/*) /bin/rm -rf -- "$path" ;;
        *) /usr/bin/rmdir -- "$path" ;;
    esac
done
EOF

chmod +x "$FAKEBIN"/*
export PATH="$FAKEBIN:$PATH"

GADGET="$KVMCTL_CONFIGFS_ROOT/usb_gadget/$KVMCTL_GADGET_NAME"

TEST_CREATE_UDC_ON_SLEEP=1 "$SCRIPT_DIR/setup-gadget.sh" up
test "$(cat "$GADGET/UDC")" = "$TEST_UDC"
test "$(cat "$GADGET/functions/hid.usb0/report_length")" = 8
test "$(cat "$GADGET/functions/hid.usb1/report_length")" = 6
test "$(cat "$GADGET/functions/hid.usb2/report_length")" = 4
test -s "$GADGET/functions/hid.usb0/report_desc"
test -e "$GADGET/configs/c.1/hid.usb0"

# A complete existing gadget is idempotent.
"$SCRIPT_DIR/setup-gadget.sh" up

# An incomplete residual gadget is removed before being rebuilt.
"$SCRIPT_DIR/setup-gadget.sh" down
/usr/bin/mkdir -p "$GADGET/functions/hid.usb0"
: > "$GADGET/UDC"
"$SCRIPT_DIR/setup-gadget.sh" up
test "$(cat "$GADGET/UDC")" = "$TEST_UDC"
"$SCRIPT_DIR/setup-gadget.sh" down
test ! -e "$GADGET"

# Any creation failure rolls the partial gadget back.
if KVMCTL_FAIL_LN=1 "$SCRIPT_DIR/setup-gadget.sh" up; then
    echo "setup unexpectedly succeeded while link creation was failing" >&2
    exit 1
fi
test ! -e "$GADGET"

# A missing UDC fails at the configured deadline without leaving a gadget.
rm -f "$KVMCTL_UDC_CLASS/$TEST_UDC" "$KVMCTL_DEV_ROOT"/hidg?
if KVMCTL_UDC_WAIT_SECONDS=0 "$SCRIPT_DIR/setup-gadget.sh" up; then
    echo "setup unexpectedly succeeded without a UDC" >&2
    exit 1
fi
test ! -e "$GADGET"

echo "setup-gadget tests passed"

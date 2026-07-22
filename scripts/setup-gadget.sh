#!/bin/bash
set -Eeuo pipefail

CONFIGFS_ROOT=${KVMCTL_CONFIGFS_ROOT:-/sys/kernel/config}
UDC_CLASS=${KVMCTL_UDC_CLASS:-/sys/class/udc}
DEV_ROOT=${KVMCTL_DEV_ROOT:-/dev}
GADGET_NAME=${KVMCTL_GADGET_NAME:-kvmctl}
UDC_WAIT_SECONDS=${KVMCTL_UDC_WAIT_SECONDS:-60}
GADGET="$CONFIGFS_ROOT/usb_gadget/$GADGET_NAME"
ACTION=${1:-up}

require_root() {
    if [ "$(id -u)" -ne 0 ]; then
        echo "setup-gadget must run as root" >&2
        exit 1
    fi
}

select_udc_once() {
    if [ -n "${KVMCTL_UDC:-}" ]; then
        [ -e "$UDC_CLASS/$KVMCTL_UDC" ] || return 1
        printf '%s' "$KVMCTL_UDC"
        return
    fi
    set -- "$UDC_CLASS"/*
    [ -e "$1" ] || return 1
    [ "$#" -eq 1 ] || {
        echo "multiple USB Device Controllers found; set KVMCTL_UDC" >&2
        return 2
    }
    basename "$1"
}

wait_for_udc() {
    case "$UDC_WAIT_SECONDS" in
        ''|*[!0-9]*)
            echo "KVMCTL_UDC_WAIT_SECONDS must be a non-negative integer" >&2
            return 1
            ;;
    esac

    deadline=$((SECONDS + UDC_WAIT_SECONDS))
    while true; do
        if udc=$(select_udc_once); then
            printf '%s' "$udc"
            return
        else
            status=$?
        fi
        [ "$status" -ne 2 ] || return 1
        if [ "$SECONDS" -ge "$deadline" ]; then
            if [ -n "${KVMCTL_UDC:-}" ]; then
                echo "configured KVMCTL_UDC did not appear within ${UDC_WAIT_SECONDS}s" >&2
            else
                echo "no USB Device Controller appeared within ${UDC_WAIT_SECONDS}s" >&2
            fi
            return 1
        fi
        sleep 0.2
    done
}

hid_nodes_ready() {
    [ -e "$DEV_ROOT/hidg0" ] && [ -e "$DEV_ROOT/hidg1" ] && [ -e "$DEV_ROOT/hidg2" ]
}

apply_hid_permissions() {
    chgrp kvmctl "$DEV_ROOT/hidg0" "$DEV_ROOT/hidg1" "$DEV_ROOT/hidg2"
    chmod 0660 "$DEV_ROOT/hidg0" "$DEV_ROOT/hidg1" "$DEV_ROOT/hidg2"
}

write_keyboard_descriptor() {
    printf '\x05\x01\x09\x06\xa1\x01\x05\x07\x19\xe0\x29\xe7\x15\x00\x25\x01\x75\x01\x95\x08\x81\x02\x95\x01\x75\x08\x81\x01\x95\x05\x75\x01\x05\x08\x19\x01\x29\x05\x91\x02\x95\x01\x75\x03\x91\x01\x95\x06\x75\x08\x15\x00\x25\x65\x05\x07\x19\x00\x29\x65\x81\x00\xc0' > "$GADGET/functions/hid.usb0/report_desc"
}

write_absolute_descriptor() {
    printf '\x05\x01\x09\x02\xa1\x01\x09\x01\xa1\x00\x05\x09\x19\x01\x29\x03\x15\x00\x25\x01\x95\x03\x75\x01\x81\x02\x95\x01\x75\x05\x81\x01\x05\x01\x09\x30\x09\x31\x16\x00\x00\x26\xff\x7f\x75\x10\x95\x02\x81\x02\x09\x38\x15\x81\x25\x7f\x75\x08\x95\x01\x81\x06\xc0\xc0' > "$GADGET/functions/hid.usb1/report_desc"
}

write_relative_descriptor() {
    printf '\x05\x01\x09\x02\xa1\x01\x09\x01\xa1\x00\x05\x09\x19\x01\x29\x03\x15\x00\x25\x01\x95\x03\x75\x01\x81\x02\x95\x01\x75\x05\x81\x01\x05\x01\x09\x30\x09\x31\x09\x38\x15\x81\x25\x7f\x75\x08\x95\x03\x81\x06\xc0\xc0' > "$GADGET/functions/hid.usb2/report_desc"
}

bring_up() {
    modprobe libcomposite
    mountpoint -q "$CONFIGFS_ROOT" || mount -t configfs none "$CONFIGFS_ROOT"

    if [ -d "$GADGET" ] && [ -s "$GADGET/UDC" ] && hid_nodes_ready; then
        apply_hid_permissions
        return
    fi
    if [ -e "$GADGET" ]; then
        echo "removing incomplete $GADGET_NAME gadget" >&2
        bring_down
    fi

    udc=$(wait_for_udc)
    trap rollback_gadget ERR
    mkdir "$GADGET"
    printf '0x1d6b' > "$GADGET/idVendor"
    printf '0x0104' > "$GADGET/idProduct"
    printf '0x0200' > "$GADGET/bcdUSB"
    printf '0x0100' > "$GADGET/bcdDevice"

    mkdir "$GADGET/strings/0x409"
    printf '0001' > "$GADGET/strings/0x409/serialnumber"
    printf 'kvmctl project' > "$GADGET/strings/0x409/manufacturer"
    printf 'kvmctl keyboard and dual pointer' > "$GADGET/strings/0x409/product"

    mkdir "$GADGET/configs/c.1" "$GADGET/configs/c.1/strings/0x409"
    printf '0xC0' > "$GADGET/configs/c.1/bmAttributes"
    printf '1' > "$GADGET/configs/c.1/MaxPower"
    printf 'HID configuration' > "$GADGET/configs/c.1/strings/0x409/configuration"

    mkdir "$GADGET/functions/hid.usb0"
    printf '1' > "$GADGET/functions/hid.usb0/protocol"
    printf '1' > "$GADGET/functions/hid.usb0/subclass"
    printf '8' > "$GADGET/functions/hid.usb0/report_length"
    write_keyboard_descriptor

    mkdir "$GADGET/functions/hid.usb1"
    printf '2' > "$GADGET/functions/hid.usb1/protocol"
    printf '0' > "$GADGET/functions/hid.usb1/subclass"
    printf '6' > "$GADGET/functions/hid.usb1/report_length"
    write_absolute_descriptor

    mkdir "$GADGET/functions/hid.usb2"
    printf '2' > "$GADGET/functions/hid.usb2/protocol"
    printf '0' > "$GADGET/functions/hid.usb2/subclass"
    printf '4' > "$GADGET/functions/hid.usb2/report_length"
    write_relative_descriptor

    ln -s "$GADGET/functions/hid.usb0" "$GADGET/configs/c.1/"
    ln -s "$GADGET/functions/hid.usb1" "$GADGET/configs/c.1/"
    ln -s "$GADGET/functions/hid.usb2" "$GADGET/configs/c.1/"
    printf '%s' "$udc" > "$GADGET/UDC"

    attempt=0
    while [ "$attempt" -lt 50 ]; do
        if hid_nodes_ready; then
            apply_hid_permissions
            trap - ERR
            return
        fi
        attempt=$((attempt + 1))
        sleep 0.1
    done
    echo "HID gadget nodes did not appear" >&2
    exit 1
}

bring_down() {
    [ -d "$GADGET" ] || return 0
    if [ -e "$GADGET/UDC" ]; then
        printf '' > "$GADGET/UDC" || true
    fi
    rm -f "$GADGET/configs/c.1/hid.usb0" "$GADGET/configs/c.1/hid.usb1" "$GADGET/configs/c.1/hid.usb2"
    rmdir "$GADGET/functions/hid.usb0" "$GADGET/functions/hid.usb1" "$GADGET/functions/hid.usb2" 2>/dev/null || true
    rmdir "$GADGET/configs/c.1/strings/0x409" "$GADGET/configs/c.1" 2>/dev/null || true
    rmdir "$GADGET/strings/0x409" 2>/dev/null || true
    rmdir "$GADGET" 2>/dev/null || {
        echo "unable to remove incomplete $GADGET_NAME gadget" >&2
        return 1
    }
}

rollback_gadget() {
    status=$?
    trap - ERR
    bring_down || true
    exit "$status"
}

require_root
case "$ACTION" in
    up) bring_up ;;
    down) bring_down ;;
    *) echo "usage: setup-gadget.sh up|down" >&2; exit 2 ;;
esac

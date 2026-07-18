#!/bin/bash
set -euo pipefail

GADGET=/sys/kernel/config/usb_gadget/kvmctl
ACTION=${1:-up}

require_root() {
    if [ "$(id -u)" -ne 0 ]; then
        echo "setup-gadget must run as root" >&2
        exit 1
    fi
}

select_udc() {
    if [ -n "${KVMCTL_UDC:-}" ]; then
        [ -e "/sys/class/udc/$KVMCTL_UDC" ] || {
            echo "configured KVMCTL_UDC does not exist" >&2
            exit 1
        }
        printf '%s' "$KVMCTL_UDC"
        return
    fi
    set -- /sys/class/udc/*
    [ -e "$1" ] || {
        echo "no USB Device Controller found" >&2
        exit 1
    }
    [ "$#" -eq 1 ] || {
        echo "multiple USB Device Controllers found; set KVMCTL_UDC" >&2
        exit 1
    }
    basename "$1"
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
    mountpoint -q /sys/kernel/config || mount -t configfs none /sys/kernel/config
    [ ! -e "$GADGET" ] || {
        echo "kvmctl gadget already exists" >&2
        exit 1
    }
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
    select_udc > "$GADGET/UDC"

    attempt=0
    while [ "$attempt" -lt 50 ]; do
        if [ -e /dev/hidg0 ] && [ -e /dev/hidg1 ] && [ -e /dev/hidg2 ]; then
            chgrp kvmctl /dev/hidg0 /dev/hidg1 /dev/hidg2
            chmod 0660 /dev/hidg0 /dev/hidg1 /dev/hidg2
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
    printf '' > "$GADGET/UDC"
    rm -f "$GADGET/configs/c.1/hid.usb0" "$GADGET/configs/c.1/hid.usb1" "$GADGET/configs/c.1/hid.usb2"
    rmdir "$GADGET/functions/hid.usb0" "$GADGET/functions/hid.usb1" "$GADGET/functions/hid.usb2"
    rmdir "$GADGET/configs/c.1/strings/0x409" "$GADGET/configs/c.1"
    rmdir "$GADGET/strings/0x409" "$GADGET"
}

require_root
case "$ACTION" in
    up) bring_up ;;
    down) bring_down ;;
    *) echo "usage: setup-gadget.sh up|down" >&2; exit 2 ;;
esac

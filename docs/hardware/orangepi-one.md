# 香橙派 One 接线与首次验收

香橙派 One 是首版唯一的一级支持硬件。代码和部署资产已针对 32 位 ARMv7、Linux V4L2 与 ConfigFS USB Gadget 构建，但在标记稳定发布前仍必须在真实板卡完成本页验收。

## 接线边界

控制设备需要三条独立链路：

1. 使用稳定的 5V 电源单独给香橙派供电；不要假设被控主机的 USB 口负责整板供电。
2. 被控主机 HDMI 输出连接到 UVC/V4L2 采集设备，再连接香橙派的 USB Host 口。
3. 香橙派的 USB OTG/Device 数据口连接被控主机 USB Host 口，承载模拟键盘和双指针。

第三条链路必须是包含数据线芯、能让香橙派进入 USB Device 模式的线。连接器外形相同不代表支持数据；禁止使用两个 USB-A Host 端直接相连。上电前先确认板卡电源口与 OTG 数据口的角色。

## 系统前提

- Linux 内核提供 ConfigFS、`libcomposite`、UDC 和 `usb_f_hid`；
- `/sys/class/udc/` 中恰好有一个可用控制器，多个时通过 `KVMCTL_UDC` 明确选择；
- `kvmctl-gadget.service` 会在启动时等待 UDC 最多 60 秒，并在创建失败时清理不完整的 ConfigFS gadget；可通过 `KVMCTL_UDC_WAIT_SECONDS` 调整等待时间；
- 采集设备出现在 `/dev/v4l/by-id/` 或 `/dev/video*`；
- 对启动时先输出测试图的采集设备，通过 `screen.warmup_frames` 丢弃预热帧；默认值为 90；
- 长期服务用户只通过 `video` 组与 Gadget 脚本授予的设备权限访问硬件。

## 已完成的真机验收基线

2026-07-21 在香橙派 One 真板完成了当前部署验收，后续硬件相关改动应至少与这组基线对照：

- 板卡与体系结构：Xunlong Orange Pi One，Allwinner H3，ARMv7；
- 系统与内核：Armbian，`Linux 6.18.33-current-sunxi`；
- 设备树：`/boot/dtb/sun8i-h3-orangepi-one.dtb`，兼容串为 `xunlong,orangepi-one`、`allwinner,sun8i-h3`；
- USB Device 控制器：设备树节点 `soc/usb@1c19000` 的 `dr_mode` 为 `otg`，运行时 UDC 为 `musb-hdrc.4.auto`，驱动为 `musb-hdrc`，模式为 `b_peripheral`；
- 实际连接：独立 5V 电源供电，HDMI 经 USB 采集卡进入 USB Host，板载 OTG 数据口连接被控主机 USB Host；
- Gadget 结果：UDC 状态为 `configured`，`/dev/hidg0`、`/dev/hidg1`、`/dev/hidg2` 均生成并归属 `root:kvmctl`。

## 验收顺序

```shell
sudo kvmctl config validate --file /etc/kvmctl/config.toml
sudo kvmctl devices screen
sudo kvmctl doctor
sudo systemctl start kvmctl-gadget.service
ls -l /dev/hidg0 /dev/hidg1 /dev/hidg2
sudo systemctl start kvmctl.service
```

然后依次验证：

1. 被控主机枚举出一个键盘、一个绝对指针和一个相对指针。
2. `kvmctl screen --view coordinates --output screen.jpg` 返回当前画面，断开 HDMI 后明确失败而不是返回旧图。
3. `key press`、绝对点击、相对移动均在动作结束后释放所有状态。
4. 两个 MCP Session 同时连接时，第二个输入得到 `CONTROL_BUSY`，但仍可调用 `get_screen`。
5. 在 BIOS/UEFI 与操作系统中分别验证绝对指针；不支持时使用相对指针。

真实验收前不要把“能够交叉编译”描述为“硬件已经支持”。验收发现的内核、设备树或供电差异应记录到 issue，并注明具体镜像与内核版本。

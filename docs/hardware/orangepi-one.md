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
- 采集设备出现在 `/dev/v4l/by-id/` 或 `/dev/video*`；
- 长期服务用户只通过 `video` 组与 Gadget 脚本授予的设备权限访问硬件。

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

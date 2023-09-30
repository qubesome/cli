package firecracker

var configTmpl = `{
    "boot-source": {
      "kernel_image_path": "{{.KernelImagePath}}",
      "boot_args": "keep_bootcon console=ttyS0 reboot=k panic=1 pci=off",
      "initrd_path": null
    },
    "drives": [
      {
        "drive_id": "rootfs",
        "path_on_host": "{{.RootFsPath}}",
        "is_root_device": true,
        "is_read_only": false,
        "partuuid": null,
        "cache_type": "Unsafe",
        "io_engine": "Sync",
        "rate_limiter": null
      }
    ],
    "machine-config": {
      "vcpu_count": 2,
      "mem_size_mib": 256,
      "smt": false,
      "track_dirty_pages": false
    },
    "cpu-config": null,
    "balloon": null,
    "network-interfaces": [
        {
            "iface_id": "net1",
            "guest_mac": "06:00:AC:10:00:02",
            "host_dev_name": "{{.HostDeviceName}}"
        }
    ],
    "vsock": null,
    "logger": null,
    "metrics": null,
    "mmds-config": null,
    "entropy": null
  }
`

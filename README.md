﻿# Label Printer

<p style="color:orange; font-weight:bold;">⚠️Warning: This is an active repo; expect breaking changes and use for reference only!⚠️</p>

Wraps [brother_ql](https://github.com/pklaus/brother_ql) in a Go server for remote operation.  It is tightly coupled to AWS Parameter Store at the moment. I probably won't have time to help you with that if you get stuck!

## Prerequisites
- [Podman](https://docs.podman.io/en/latest/) `sudo apt install podman`
- AWS credentials
- A compatible printer (see [brother_ql's README](https://github.com/pklaus/brother_ql))

## Install
### Debian (Tested on Ubuntu)

1. Create the container definition in `/etc/containers/systemd/label-printer-server.container`

```ini
[Unit]
Description=Label Printer Server

[Container]
Image=ghcr.io/control-alt-repeat/label-printer/server:latest

Volume=/home/control-alt-repeat/.aws:/root/.aws:ro
Volume=/dev:/dev:slave

PodmanArgs=--privileged

[Service]
Restart=always

[Install]
WantedBy=multi-user.target default.target
```

2. Reload

```shell
sudo systemctl daemon-reload
```

3. Add AWS credentials. On load, the server will add the dynamically generated hostname to AWS Parameter Store.

4. Start the service
```shell
sudo systemctl start label-printer-server.service
```

5. Check it's running

```shell
journalctl -xeu label-printer-server.service
```

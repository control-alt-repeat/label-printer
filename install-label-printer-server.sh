#!/bin/bash
sudo cp config/systemd/label-printer-server.container /etc/containers/systemd/
sudo systemctl daemon-reload
sudo systemctl enable --now label-printer-server.service

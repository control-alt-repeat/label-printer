#!/bin/bash
sudo cp config/systemd/label-printer-server.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable label-printer-server.service
sudo systemctl start label-printer-server.service

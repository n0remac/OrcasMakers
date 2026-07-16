# WebRTC production provisioning

The application remains a native systemd service on port 8081. Coturn runs
beside it and receives TURN traffic directly; Nginx proxies only HTTP and WSS.

## DNS and firewall

Create an A record for `turn.orcasmaker.com` pointing to the droplet. Allow
`3478/tcp`, `3478/udp`, and `49160-49200/udp`. If the droplet is behind NAT,
forward those ports and set `external-ip` in `/etc/turnserver.conf` to the
Internet-facing IPv4 address.

## Secrets and Coturn

Install Coturn using the server distribution package, then generate two
independent secrets:

```sh
sudo install -d -m 0750 -o root -g orcasmakers /etc/orcasmakers
openssl rand -hex 32 | sudo tee /etc/orcasmakers/turn-shared-secret >/dev/null
openssl rand -hex 32 | sudo tee /etc/orcasmakers/robot-webrtc-token >/dev/null
sudo chown root:orcasmakers /etc/orcasmakers/turn-shared-secret /etc/orcasmakers/robot-webrtc-token
sudo chmod 0640 /etc/orcasmakers/turn-shared-secret /etc/orcasmakers/robot-webrtc-token
```

Copy `deploy/coturn/turnserver.conf.example` to `/etc/turnserver.conf`. Replace
the public IP and `static-auth-secret` placeholders; the latter must exactly
match `/etc/orcasmakers/turn-shared-secret`. Secure the Coturn config with mode
0640 and start `coturn.service`.

## OrcasMakers and Nginx

Install `deploy/systemd/orcasmakers-webrtc.conf` as
`/etc/systemd/system/orcasmakers.service.d/webrtc.conf`, ensuring the service
account belongs to the `orcasmakers` group. Merge `deploy/nginx/webrtc.conf`
into `/etc/nginx/sites-available/orcasmaker.com`, then validate and reload:

```sh
sudo systemctl daemon-reload
sudo nginx -t
sudo systemctl reload nginx
sudo systemctl enable --now coturn.service
sudo systemctl restart orcasmakers.service
curl --fail https://orcasmaker.com/healthz
```

Configure the Raspberry Pi with the contents of
`/etc/orcasmakers/robot-webrtc-token` as `ROBOT_WEBRTC_TOKEN`.

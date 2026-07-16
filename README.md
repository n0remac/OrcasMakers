# OrcasMakers

## Open Sauce project board

The Open Sauce showcase is available at `/open-sauce`. It displays the RC
truck, RedBot, Solar Hat, CoolerMobile, and Bike Trailer projects in automatic
media slideshows. Images and videos are stored by project in
`open-sauce-media/` and served beneath `/open-sauce/media/`.

The media directory is deployed beside the application binary. Keep that
directory present when running a packaged binary outside this repository.

## Deployment

Pushes to `main` build the Linux binary, package `open-sauce-media/`, and deploy
both to `/srv/orcasmakers/app`. Configure `DROPLET_HOST`, `DROPLET_USER`, and
`DROPLET_SSH_KEY` as GitHub Actions secrets.

The SSH deploy user must own the application directory so binary and media
updates do not require root privileges. Replace the placeholders with the
account and its primary group:

```bash
sudo chown -R <deploy-user>:<deploy-group> /srv/orcasmakers/app
sudo chmod 0755 /srv/orcasmakers/app
```

The workflow only needs elevated privileges to restart the existing systemd
service. Create a command-specific sudoers entry:

```bash
sudo visudo -f /etc/sudoers.d/orcasmakers-deploy
```

Add:

```sudoers
<deploy-user> ALL=(root) NOPASSWD: /usr/bin/systemctl restart orcasmakers.service
```

Secure and validate the entry:

```bash
sudo chmod 0440 /etc/sudoers.d/orcasmakers-deploy
sudo visudo -cf /etc/sudoers.d/orcasmakers-deploy
```

From a login shell for the deploy user, verify both required permissions before
running the workflow:

```bash
test -w /srv/orcasmakers/app
sudo -n /usr/bin/systemctl restart orcasmakers.service
```

The deployment does not run `systemctl daemon-reload` because it does not modify
the service unit.

The robot controller is available at `/robot`. The Raspberry Pi connects to
`/ws/robot` and sends camera frames while receiving control messages, so the Pi
does not need to expose a web server to the internet. The controller and camera
stream are publicly accessible at `/robot`.

## Robot configuration

- Add the same `ROBOT_TOKEN` GitHub secret to this repository and the robot's
  runtime environment. Production deployments reject robot connections when
  the token is missing or incorrect.
- Add `ROBOT_SITE_URL` (for example `https://makers.example.com`) as a GitHub
  secret in the `robot-webrtc` repository. Its ARM build workflow injects that
  URL into the production binary.
- Local robot builds default to `http://localhost:8081`. You can override that
  with the `ROBOT_SITE_URL` environment variable or the `-site-url` flag.

## Car dashboard

The authenticated dashboard is available at `/car`. Any signed-in user can see
telemetry; an administrator account is required to arm, command, or stop the
car. Only one administrator can own the controller at a time. The browser sends
a command heartbeat every 100 ms, the server disarms after 500 ms without one,
and the device is shown offline after two seconds without a valid sync. Active
control is memory-only and every server restart starts disarmed.

Configure a long random secret as `CAR_DEVICE_TOKEN`, or set
`CAR_DEVICE_TOKEN_FILE` to a file containing it. If no file is specified,
`car-device-token` is checked. Production (`ENVIRONMENT=production`) rejects all
device sync requests when no token is configured. Store the token in the
deployment secret store; do not commit it.

The ESP32 sends `POST /api/car/device/sync` with `Authorization: Bearer <token>`
and a JSON body matching this version 1 contract:

```json
{
  "protocol_version": 1,
  "speed": 12.5,
  "rpm": 900,
  "gear": "D",
  "fuel_percent": 75,
  "headlights": true,
  "turn_signal": "off",
  "temperature": 21.0,
  "humidity": 50.0,
  "pressure": 1013.2,
  "altitude": 12.0,
  "pitch": 1.0,
  "roll": 2.0,
  "acceleration": {"x": 0, "y": 0, "z": 9.8},
  "gyro": {"x": 0, "y": 0, "z": 0},
  "gps": {"fix": true, "latitude": 48.69, "longitude": -122.91, "satellites": 8, "speed": 12.5},
  "receiver": "remote"
}
```

Units are km/h, Celsius, hPa, metres, degrees, m/s², and degrees/second as
appropriate. `turn_signal` is `off`, `left`, `right`, or `hazard`. The response
is the current command:

```json
{"armed":true,"session_id":"...","sequence":12,"generation":3,"steering":-25,"throttle":40}
```

### ESP32 safety requirements

The ESP32 must treat the server response as untrusted input, clamp steering and
throttle to `-100..100`, and drive only when `armed` is true. It must immediately
apply neutral output when a request fails, JSON is invalid, the session or
generation changes unexpectedly, or no fresh successful armed response has
arrived within 500 ms. This independent hardware-side watchdog is mandatory:
the server cannot stop the motors during Wi-Fi loss, process failure, or a
partitioned connection. Start integration with the drive hardware disconnected,
then test the watchdog with Wi-Fi and power interruptions before ground driving.

Production use also requires a hostname with valid HTTPS. Plain HTTP exposes
both the bearer token and vehicle commands. Redirect browser HTTP traffic to
HTTPS and avoid logging authorization headers or complete request bodies.

# OrcasMakers

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

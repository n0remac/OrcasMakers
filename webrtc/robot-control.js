const ROOM = "robot";

let myUUID = generateUUID();
let ws;
let pc;
let dc;
let globalIceServers = [];
const ROBOT_ID = "robot";
const pressedKeys = new Set();
const statusEl = document.getElementById('webrtc-status');
let reconnectTimer;
let started = false;

function setStatus(text, kind = 'warning') {
    if (!statusEl) return;
    statusEl.textContent = text;
    statusEl.className = `badge badge-${kind}`;
}

window.addEventListener('beforeunload', () => {
    releaseAllKeys();
    if (ws && ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({ 
            type: 'leave',
            from: myUUID,
            room: ROOM
        }));
        ws.close();
    }
});

async function joinSession() {
    setStatus('Fetching TURN credentials…', 'info');
    // Fetch TURN
    const turnData = await fetchTurnCredentials();
    globalIceServers = [];
    if (turnData?.username && turnData?.password && turnData?.urls?.length) {
        globalIceServers.push({
            urls: turnData.urls,
            username: turnData.username,
            credential: turnData.password
        });
    } else {
        throw new Error('TURN credentials are unavailable');
    }
    setStatus('Signaling…', 'info');
    await connectWebSocket();
}

async function connectWebSocket() {
    ws = new WebSocket(
      (location.protocol === 'https:' ? 'wss://' : 'ws://')
      + location.host
      + '/ws/webrtc?room=' + encodeURIComponent(ROOM)
      + '&playerId=' + encodeURIComponent(myUUID)
    );

    ws.onopen = () => {
      Logger.info('WebSocket open');
      setStatus('Robot offline', 'warning');
      ws.send(JSON.stringify({
        type: 'join',
        join: myUUID,
        from: myUUID,
        room: ROOM,
        to: ROBOT_ID
      }));
    };
  
    ws.onmessage = ({ data }) => {
      const msg = JSON.parse(data);
      console.log("Received message:", msg);

      if (msg.type === 'error') {
          setStatus(msg.error || 'Signaling failed', 'error');
          return;
      }

      // Only handle messages *from* the robot
      if (msg.from !== ROBOT_ID) return;

      if (!pc) {
          pc = createPeerConnection(ROBOT_ID);
      }
      pc.handleSignal(msg);
  };
  
    ws.onerror = e => {
      Logger.error('WebSocket error', e);
      setStatus('Signaling failed', 'error');
    };
    ws.onclose = (e) => {
      releaseAllKeys();
      if (pc) {
        pc.close();
        pc = null;
        Logger.info("Closed PeerConnection due to signaling disconnect");
      }
      if (e.code !== 1000) {
        Logger.info('Trying to reconnect...');
        setStatus('Disconnected — retrying…', 'warning');
        clearTimeout(reconnectTimer);
        reconnectTimer = setTimeout(connectWebSocket, 1000);
      }
    };
}

function createPeerConnection(peerId) {
    const pc = new RTCPeerConnection({ iceServers: globalIceServers });
    pc.makingOffer = false;
    pc.ignoreOffer = false;
    const polite = myUUID < peerId;
    let negotiating = false;

    // create an *outgoing* data‐channel
    dc = pc.createDataChannel('keyboard');

    dc.onopen = () => {
      Logger.info('keyboard DataChannel open', { peer: peerId });
      setStatus('Connected', 'success');
    };
    dc.onclose = () => {
      releaseAllKeys();
      setStatus('Control channel closed', 'warning');
    };

    // accept an *incoming* data‐channel
    pc.ondatachannel = ({ channel }) => {
      dc = channel;
      channel.onopen = () => {
        Logger.info('incoming channel open', { peer: peerId });
        setStatus('Connected', 'success');
      };
      channel.onclose = () => setStatus('Control channel closed', 'warning');
    };
  
    // buffer any early ICE candidates here
    pc.queuedCandidates = [];

   let combinedStream = new MediaStream();

    pc.ontrack = ({ track, streams }) => {
        Logger.info('Track received', { kind: track.kind, streams });

        // Add the track to the combined stream
        combinedStream.addTrack(track);

        const video = document.getElementById('robot-video');
        if (!video) return;

        // Only update srcObject if it's not already the combinedStream
        if (video.srcObject !== combinedStream) {
            video.srcObject = combinedStream;
        }

        video.muted = false;
        video.play();
        setStatus('Connected', 'success');
    };


  
    pc.onnegotiationneeded = async () => {
      if (negotiating) return;
      negotiating = true;
      setStatus('Negotiating…', 'info');
      try {
        pc.makingOffer = true;
        const offer = await pc.createOffer();
        await pc.setLocalDescription(offer);
  
        ws.send(JSON.stringify({
          type: 'offer',
          offer: pc.localDescription,
          from:  myUUID,
          to:    peerId,
          room:  ROOM
        }));
        Logger.info('offer sent', { to: peerId });
      } finally {
        pc.makingOffer = false;
        negotiating = false;
      }
    };
  
    pc.onicecandidate = e => {
      if (!e.candidate) return;
      ws.send(JSON.stringify({
        type: 'candidate',
        candidate: e.candidate,
        from:      myUUID,
        to:        peerId,
        room:      ROOM
      }));
    };
  
    pc.oniceconnectionstatechange = () => {
      Logger.info('ICE connection state:', pc.iceConnectionState);
      if (pc.iceConnectionState === 'checking') setStatus('Checking connection…', 'info');
      if (pc.iceConnectionState === 'connected' || pc.iceConnectionState === 'completed') updateConnectionType(pc);
      if (pc.iceConnectionState === 'failed') {
        setStatus('Connection failed — restarting ICE…', 'error');
        pc.createOffer({ iceRestart: true })
          .then(o => pc.setLocalDescription(o))
          .then(() => ws.send(JSON.stringify({
            type: 'offer',
            offer: pc.localDescription,
            from:  myUUID,
            to:    peerId,
            room:  ROOM,
            name: 'robot'
          })));
      }
      if (pc.iceConnectionState === 'disconnected' || pc.iceConnectionState === 'closed') {
        // Optional: attempt recovery or notify user
        Logger.warn('ICE disconnected, consider retrying or alerting user');
        releaseAllKeys();
        setStatus('Robot disconnected', 'warning');
      }
    };
  
    pc.handleSignal = async msg => {
        console.log("Handling signal:", msg);
        switch (msg.type) {
          case 'offer':
            console.log("Received offer from robot, processing…");
            const collision = pc.makingOffer || pc.signalingState !== 'stable';
            pc.ignoreOffer = !polite && collision;
            if (pc.ignoreOffer) return;
            if (collision) await pc.setLocalDescription({ type: 'rollback' });
    
            try {
              await pc.setRemoteDescription(msg.offer);
              Logger.info('Set remote description OK');
            } catch (e) {
              Logger.error('Failed to set remote description', e);
            }
            Logger.info('Local SDP:', pc.localDescription?.sdp);
            Logger.info('Remote SDP:', pc.remoteDescription?.sdp);

            // flush any queued ICE candidates
            pc.queuedCandidates.forEach(c => pc.addIceCandidate(c));
            pc.queuedCandidates = [];
    
            const answer = await pc.createAnswer();
            await pc.setLocalDescription(answer);

            console.log("Received offer from robot, sending answer…");
            ws.send(JSON.stringify({
              type:   'answer',
              answer: pc.localDescription,
              from:   myUUID,
              to:     peerId,
              room:   ROOM,
              name:   'robot'
            }));
            Logger.info('answer sent', { to: peerId });
            break;
        case 'answer':
          if (!pc.makingOffer && pc.signalingState === 'have-local-offer') {
            await pc.setRemoteDescription(msg.answer);
            // flush queued candidates
            pc.queuedCandidates.forEach(c => pc.addIceCandidate(c));
            pc.queuedCandidates = [];
            Logger.info('remote SDP applied', { peer: peerId });
          }
          break;
        case 'candidate':
          console.log("Received ICE candidate from robot, processing…");
          // if remoteDescription isn’t set yet, queue it
          if (!pc.remoteDescription) {
            pc.queuedCandidates.push(msg.candidate);
          } else {
            try {
              await pc.addIceCandidate(msg.candidate);
            } catch (e) {
              console.warn('failed to add ICE candidate', e);
            }
          }
          break;
        case 'leave':
          Logger.info('handling leave signal', { from: msg.from });
          handleUserDisconnect(msg.from);
          break;
      }
    }
  
  return pc;
}

function handleUserDisconnect(uuid) {
    Logger.info('disconnecting peer', { peer: uuid });
    const video = document.getElementById(`robot-video`);
    if (!video) Logger.warn('no video found for peer', { peer: uuid });
    if (video) video.srcObject = null;
    if (pc) {
        pc.close();
        pc = null;
        Logger.info('closing peer connection', { peer: uuid });
      } else {
        Logger.warn('no peer connection found', { peer: uuid });
      }
    releaseAllKeys();
    setStatus('Robot offline', 'warning');
}

function generateUUID() {
    // If available, use the browser's native randomUUID
    if (window.crypto && window.crypto.randomUUID) {
        return window.crypto.randomUUID();
    }
    // Otherwise, polyfill
    const hex = [];
    const rnds = new Uint8Array(16);
    window.crypto.getRandomValues(rnds);
    rnds[6] = (rnds[6] & 0x0f) | 0x40; // version 4
    rnds[8] = (rnds[8] & 0x3f) | 0x80; // variant 10xx

    for (let i = 0; i < 16; i++) {
        hex.push(rnds[i].toString(16).padStart(2, '0'));
    }
    return [
        hex.slice(0, 4).join(''),
        hex.slice(4, 6).join(''),
        hex.slice(6, 8).join(''),
        hex.slice(8, 10).join(''),
        hex.slice(10, 16).join('')
    ].join('-');
}

function bindkeys() {
    // bind keys
    ;[
      'w','a','s','d', 
      't', 'f', 'g', 'h',
      'i', 'j', 'k', 'l',
      'r', 'y',
    ].forEach(k =>
      createKeyPressEventListener(k)
    );
}

function createKeyPressEventListener(key) {
  const normalized = key.toLowerCase();

  function handler(e) {
    if (e.key && e.key.toLowerCase() === normalized) {
      e.preventDefault();
      e.stopImmediatePropagation();

      const action = e.type === 'keydown' ? 'pressed' : 'released';
      if (action === 'pressed' && pressedKeys.has(normalized)) return;
      if (action === 'released' && !pressedKeys.has(normalized)) return;
      action === 'pressed' ? pressedKeys.add(normalized) : pressedKeys.delete(normalized);
      console.log(`Key ${normalized} ${action}`);

      // broadcast to each peer
      if (dc && dc.readyState === 'open') {
        console.log(`Sending ${action} event to ${dc.label}`);
        dc.send(JSON.stringify({ key: normalized, action }));
      }
    }
  }

  window.addEventListener('keydown', handler, true);
  window.addEventListener('keyup',   handler, true);
}

function releaseAllKeys() {
  if (!dc || dc.readyState !== 'open') {
    pressedKeys.clear();
    return;
  }
  [...pressedKeys].forEach(key => {
    dc.send(JSON.stringify({ key, action: 'released' }));
    pressedKeys.delete(key);
  });
}

async function updateConnectionType(connection) {
  try {
    const stats = await connection.getStats();
    let selectedPair;
    stats.forEach(report => {
      if (report.type === 'transport' && report.selectedCandidatePairId) selectedPair = stats.get(report.selectedCandidatePairId);
      if (report.type === 'candidate-pair' && report.selected) selectedPair = report;
    });
    const local = selectedPair && stats.get(selectedPair.localCandidateId);
    setStatus(local?.candidateType === 'relay' ? 'Connected via TURN relay' : 'Connected directly', 'success');
  } catch (_) {
    setStatus('Connected', 'success');
  }
}

function start() {
    joinSession();
}

document.addEventListener("DOMContentLoaded", () => {
    bindkeys();
    document.getElementById('start-video-btn').addEventListener('click', () => {
        Logger.info('Start Video button clicked');
        if (started) return;
        started = true;
        joinSession().catch(error => {
          Logger.error('Unable to start WebRTC session', error);
          setStatus(error.message || 'Connection failed', 'error');
          started = false;
          document.getElementById('start-video-btn').disabled = false;
          document.getElementById('start-video-btn').textContent = 'Retry connection';
        });
        // Optionally, disable the button to prevent double start
        document.getElementById('start-video-btn').disabled = true;
        document.getElementById('start-video-btn').textContent = 'Connecting...';
    });
});

window.addEventListener('blur', releaseAllKeys);
document.addEventListener('visibilitychange', () => { if (document.hidden) releaseAllKeys(); });
setInterval(() => {
  if (dc?.readyState === 'open') dc.send(JSON.stringify({type: 'heartbeat'}));
}, 250);

function triggerKeyEvent(key, type) {
    const event = new KeyboardEvent(type, {
        key: key,
        bubbles: true,
        cancelable: true,
    });
    window.dispatchEvent(event);
}

// Add event listeners for the control buttons after DOM loads
window.addEventListener('DOMContentLoaded', () => {
    document.querySelectorAll('.control-btn').forEach(btn => {
        const key = btn.dataset.key;
        btn.addEventListener('mousedown', e => {
            triggerKeyEvent(key, 'keydown');
        });
        btn.addEventListener('mouseup', e => {
            triggerKeyEvent(key, 'keyup');
        });
        btn.addEventListener('mouseleave', e => {
            triggerKeyEvent(key, 'keyup'); // handle mouse out
        });
        // For accessibility / touch
        btn.addEventListener('touchstart', e => {
            e.preventDefault();
            triggerKeyEvent(key, 'keydown');
        }, {passive: false});
        btn.addEventListener('touchend', e => {
            e.preventDefault();
            triggerKeyEvent(key, 'keyup');
        }, {passive: false});
    });
});

async function fetchTurnCredentials() {
    try {
        const res = await fetch('/webrtc/turn-credentials?user=' + encodeURIComponent(myUUID));
        if (!res.ok) {
            console.error('Failed to fetch turn credentials:', res.status, res.statusText);
            return null;
        }
        const text = await res.text();
        try {
            return JSON.parse(text);
        } catch (err) {
            console.error('Turn credentials are not JSON:', text);
            return null;
        }
    } catch (e) {
        console.error('Error fetching turn credentials:', e);
        return null;
    }
}

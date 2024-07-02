const Config = {
    APIEndpoint: "/api/room",
    SignalingEndpoint: "wss://192.168.1.122/signal",
    RTCConfig: {
        iceServers: [
            {
                urls: ["stun:stun1.l.google.com:19302", "stun:stun2.l.google.com:19302"]
            }
        ]
    },
    UserMediaConfig: {
        video: {
            width: {min:640, max:1920},
            height: {min:480, max:1080}
        },
        audio: true
    }
}

function init() {
    const joinForm = document.getElementById("join-form")

    const cameraBtn = document.getElementById("camera-btn")
    const cameraOffBtn = document.getElementById("camera-off-btn")

    const micBtn = document.getElementById("mic-btn")
    const micOffBtn = document.getElementById("mic-off-btn")

    const leaveBtn = document.getElementById("leave-btn")

    const lobby = document.getElementById("lobby")
    const room = document.getElementById("room")

    const videoElementLocal = document.getElementById("user-1")
    const videoElementRemote = document.getElementById("user-2")

    let signaling;

    joinForm.addEventListener("submit", async (e) => {
        e.preventDefault()

        const params = await prepareCall()

        if (params) {
            lobby.style.display = 'none'
            room.style.display = 'block'

            const [localStream, remoteStream] = await createStreams(videoElementLocal, videoElementRemote)

            const toggleCamera = async (e) => {
                const track = localStream.getTracks().find(track => track.kind === "video")
                if (track) {
                    track.enabled = !track.enabled
                }
            }

            const toggleMic = async (e) => {
                const track = localStream.getTracks().find(track => track.kind === "audio")
                if (track) {
                    track.enabled = !track.enabled
                }
            }

            cameraBtn.addEventListener("click", async (e) => {
                await toggleCamera()
                cameraOffBtn.style.display = "block"
                cameraBtn.style.display = "none"
            })
            cameraOffBtn.addEventListener("click", async (e) => {
                await toggleCamera()
                cameraOffBtn.style.display = "none"
                cameraBtn.style.display = "block"
            })

            micBtn.addEventListener("click", async (e) => {
                await toggleMic()
                micOffBtn.style.display = "block"
                micBtn.style.display = "none"
            })
            micOffBtn.addEventListener("click", async (e) => {
                await toggleMic()
                micOffBtn.style.display = "none"
                micBtn.style.display = "block"
            })

            signaling = await startCall(params, localStream, remoteStream, videoElementLocal);
        }
    })

    leaveBtn.addEventListener("click", (e) => {
        lobby.style.display = 'block'
        room.style.display = 'none'

        if (signaling) {
            signaling.stop()
        }
    })
}

function showRemoteVideo(yes) {
    if (yes) {
        document.getElementById("user-2").style.display = 'block';
    } else {
        document.getElementById("user-2").style.display = 'none';
    }
}

async function prepareCall() {
    const myID = document.getElementById("user-name").value
    if (myID === "") {
        console.log("empty username")
        alert("empty username")
        return
    }
    const roomID = document.getElementById("room-code").value
    if (roomID === "") {
        console.log("empty room code")
        alert("empty room code")
        return
    }

    const resp = await joinRoom(myID, roomID)
    if (resp.message !== "OK") {
        alert("unable to join room: " + resp.error)
        console.log("unable to join the room", resp.error)
        return
    }
    console.log("successfully joined the room")
    return {userID: myID, roomID: roomID}
}

async function startCall(params, localStream, remoteStream, videoElementLocal) {
    const signaling = buildSignaling(params.roomID, params.userID, localStream, remoteStream, videoElementLocal)
    await signaling.start()
    return signaling
}

async function createStreams(videoElementLocal, videoElementRemote) {
    console.log("supported constraints:", navigator.mediaDevices.getSupportedConstraints())
    navigator.mediaDevices.enumerateDevices().then((devices) => {
        devices.forEach((device) => {
            console.log("device:", device);
            if (device.getCapabilities) {
                console.log("device capabilities:", device.getCapabilities());
            }
        });
      });
    const localStream = await navigator.mediaDevices.getUserMedia(Config.UserMediaConfig)
    videoElementLocal.srcObject = localStream

    const remoteStream = new MediaStream()
    videoElementRemote.srcObject = remoteStream

    return [localStream, remoteStream]
}

async function joinRoom(myID, roomID) {
    const joinParams = {
        "room_id": roomID,
        "user_id": myID,
    }
    const response = await fetch(Config.APIEndpoint, {
        method: "POST",
        cache: "no-cache",
        headers: {
            "Content-Type": "application/json",
        },
        body: JSON.stringify(joinParams),
    })
    return response.json()
}

const buildSignaling = (roomID, myID, localStream, remoteStream, videoElementLocal) => {
    const logPref = `[signaling][${roomID}]`;
    const wsPath = Config.SignalingEndpoint + "/room/" + roomID + "/user/" + myID;
    let transport;
    let peers = {};

    const createPeerConnection = async (localStream, remoteStream, onicecandidate) => {
        const pc = new RTCPeerConnection(Config.RTCConfig)

        showRemoteVideo(true)

        pc.ontrack = (event) => {
            event.streams[0].getTracks().forEach((track) => {
                remoteStream.addTrack(track)
            })
        }
    
        localStream.getTracks().forEach((track) => {
            pc.addTrack(track, localStream)
        })

        pc.onicecandidate = onicecandidate

        return pc
    }

    const createOffer = async (peerConnection) => {
        const offer = await peerConnection.createOffer()
        await peerConnection.setLocalDescription(offer)
    
        console.log("offer created:", offer)
    
        return offer
    }

    const createAnswer = async(peerConnection, offer) => {
        await peerConnection.setRemoteDescription(offer);
        const answer = await peerConnection.createAnswer();
        await peerConnection.setLocalDescription(answer);
        return answer
    }

    return {
        async start(){
            transport = buildWebSocketTransport(roomID);
            window.addEventListener('beforeunload', transport.disconnect)
            transport.addListener(async function(announcement){
                console.log(`${logPref} got announcement:`, announcement)

                const remoteUserID = announcement.src;
                let pc = peers[remoteUserID]

                switch (announcement.type) {
                    case "joined":
                        // new user joined
                        // initiate peer connection
                        if (remoteUserID) {
                            videoElementLocal.classList.add("small-frame")
                            if (pc) {
                                console.log("user rejoined:", remoteUserID)
                                await pc.restartIce()
                            } else {
                                console.log("new user has joined:", remoteUserID)

                                pc = await createPeerConnection(localStream, remoteStream, async (event) => {
                                    if (event.candidate) {
                                        transport.send({
                                            dst: remoteUserID,
                                            type: "candidate",
                                            payload: event.candidate,
                                        });
                                    }
                                });
                                peers[remoteUserID] = pc
                            }
                            const offer = await createOffer(pc)
                            transport.send({
                                dst: remoteUserID,
                                type: "offer",
                                payload: offer,
                            });
                        }
                        break;
    
                    case "left":
                        // user left
                        // remove peer connection
                        if (pc) {
                            videoElementLocal.classList.remove("small-frame")
                            remoteStream.getTracks().forEach((track)=>{
                                track.stop()
                                remoteStream.removeTrack(track)
                            })
                            showRemoteVideo(false)
                            pc.close();
                            delete peers[remoteUserID]
                        }
                        break;

                    case "offer":
                        // sdp offer
                        // create peer connection if not exist
                        videoElementLocal.classList.add("small-frame")
                        if (!pc) {
                            pc = await createPeerConnection(localStream, remoteStream, async (event) => {
                                if (event.candidate) {
                                    transport.send({
                                        dst: remoteUserID,
                                        type: "candidate",
                                        payload: event.candidate,
                                    });
                                }
                            });
                            peers[remoteUserID] = pc;
                        }

                        if (announcement.payload) {
                            const answer = await createAnswer(pc, announcement.payload)
                            transport.send({
                                dst: remoteUserID,
                                type: "answer",
                                payload: answer,
                            });
                        }

                        break;

                    case "answer":
                        if (pc) {
                            await pc.setRemoteDescription(announcement.payload)
                        } else {
                            console.log(`${logPref} got answer for unknown peer: ${remoteUserID}`)
                        }
                        break;

                    case "candidate":
                        if (pc) {
                            await pc.addIceCandidate(announcement.payload)
                        } else {
                            console.log(`${logPref} got ice candidate for unknown peer: ${remoteUserID}`)
                        }

                        break;
                    default:
                        console.log(`${logPref} unknown announcement type: ${announcement.type}`)
                }
            })
            transport.connect(wsPath)
        },
        async stop() {
            showRemoteVideo(false)
            remoteStream.getTracks().forEach((track)=>{
                console.log("removing track", track)
                track.stop()
                remoteStream.removeTrack(track)
            })
            localStream.getTracks().forEach((track)=>{
                console.log("removing track", track)
                track.stop()
                localStream.removeTrack(track)
            })
            for (const remoteUserID in peers) {
                peers[remoteUserID].close();
                delete peers[remoteUserID];
            }
            transport.disconnect()
        }
    }
}

const buildWebSocketTransport = (name) => {
    let socket = null;
    let callback = null;
    let address = null;

    const logPref = `[websocket][${name}]`;

    return {
        addListener: (cb) => {
            callback = cb;
        },
        connect: (addr) => {
            address = addr;
            socket = new WebSocket(addr);

            socket.addEventListener("open", (event) => {
                console.log(`${logPref} connected to ${addr}`)
            });

            socket.addEventListener("message", async (event) => {
                if (callback) {
                    callback.call(this, JSON.parse(event.data))
                }
            });
        },
        send: (message) => {
            if (socket) {
                socket.send(JSON.stringify(message));
            }
        },
        disconnect: () => {
            if (socket) {
                socket.close()
                socket = null
                console.log(`${logPref} connection with ${address} is closed!`)
            }
        }
    }
}

document.addEventListener("DOMContentLoaded", init)

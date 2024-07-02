const Config = {
    RoomID: "test",

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
        video: true,
        audio: false
    }
}

function showRemoteVideo(yes) {
    if (yes) {
        document.getElementById("user-2").style.display = 'block';
    } else {
        document.getElementById("user-2").style.display = 'none';
    }
}

async function initCall() {
    const myID = document.getElementById("user-name").value
    if (myID === "") {
        console.log("empty username")
        alert("empty username")
        return
    }

    const resp = await joinRoom(myID, Config.RoomID)
    if (resp.message !== "OK") {
        alert("unable to join room: " + resp.error)
        console.log("unable to join the room", resp.error)
        return
    }

    console.log("successfully joined the room")

    const [localStream, remoteStream] = await createStreams()
    
    const signaling = buildSignaling(Config.RoomID, myID, localStream, remoteStream)
    await signaling.start()
}

async function createStreams() {
    const videoElementLocal = document.getElementById("user-1")
    const videoElementRemote = document.getElementById("user-2")

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

const buildSignaling = (roomID, myID, localStream, remoteStream) => {
    const logPref = `[signaling][${roomID}]`;
    const wsPath = Config.SignalingEndpoint + "/room/" + roomID + "/user/" + myID;
    let transport;

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
            let peers = {};
            transport.addListener(async function(announcement){
                console.log(`${logPref} got announcement:`, announcement)

                const remoteUserID = announcement.src;
                let pc = peers[remoteUserID]

                switch (announcement.type) {
                    case "joined":
                        // new user joined
                        // initiate peer connection
                        if (remoteUserID) {
                            if (pc) {
                                console.log("user rejoined:", remoteUserID)
                                await pc.restartIce();
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
                                peers[remoteUserID] = pc;
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
                            remoteStream.getTracks().forEach((track)=>{
                                remoteStream.removeTrack(track)
                            })
                            showRemoteVideo(false)
                            pc.close();
                            delete peers[remoteUserID];
                        }
                        break;

                    case "offer":
                        // sdp offer
                        // create peer connection if not exist
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
                            await pc.setRemoteDescription(announcement.payload);
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
                console.log(`${logPref} connected to ${addr}`);
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
                socket.close();
                socket = null;
                console.log(`${logPref} connection with ${address} is closed!`);
            }
        }
    }
}

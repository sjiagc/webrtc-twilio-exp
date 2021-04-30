package recorder

import (
	"fmt"
	"github.com/google/uuid"
	"github.com/pion/webrtc/v2"
)

type IceServer struct {
	Urls string			`json:"urls"`
	Username string		`json:"username"`
	Credential string	`json:"credential"`
}

type PeerConnection struct {
	id string
	api *webrtc.API
	pc *webrtc.PeerConnection
}


func NewPeerConnection(InIceServers []IceServer) (*PeerConnection, error) {
	var err error

	var iceServerConfigs []webrtc.ICEServer
	for _, iceServer := range InIceServers {
		iceServerConfig := webrtc.ICEServer{}
		iceServerConfig.URLs = append(iceServerConfig.URLs, iceServer.Urls)
		iceServerConfig.Username = iceServer.Username
		iceServerConfig.Credential = iceServer.Credential
		iceServerConfig.CredentialType = webrtc.ICECredentialTypePassword
		iceServerConfigs = append(iceServerConfigs, iceServerConfig)
	}

	config := webrtc.Configuration{
		BundlePolicy: webrtc.BundlePolicyMaxBundle,
		ICEServers: iceServerConfigs,
		RTCPMuxPolicy: webrtc.RTCPMuxPolicyRequire,
		SDPSemantics: webrtc.SDPSemanticsUnifiedPlan,
	}

	pc := PeerConnection{}
	pc.id = uuid.New().String()

	se := webrtc.SettingEngine{}
	se.SetTrickle(true)
	me := webrtc.MediaEngine{}
	me.RegisterCodec(webrtc.NewRTPOpusCodec(webrtc.DefaultPayloadTypeOpus, 48000))
	me.RegisterCodec(webrtc.NewRTPVP8Codec(webrtc.DefaultPayloadTypeVP8, 90000))
	pc.api = webrtc.NewAPI(webrtc.WithSettingEngine(se), webrtc.WithMediaEngine(me))
	pc.pc, err = pc.api.NewPeerConnection(config)
	if err != nil {
		return nil, fmt.Errorf("create PeerConnectionEntity failed: %v\n", err)
	}

	videoCodecs := pc.pc.GetRegisteredRTPCodecs(webrtc.RTPCodecTypeVideo)
	fmt.Println("Registered video codec")
	for _, vc := range videoCodecs {
		fmt.Printf("%+v\n", vc)
	}
	audioCodecs := pc.pc.GetRegisteredRTPCodecs(webrtc.RTPCodecTypeAudio)
	fmt.Println("Registered audio codec")
	for _, ac := range audioCodecs {
		fmt.Printf("%+v\n", ac)
	}
	if _, err = pc.pc.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio); err != nil {
		return nil, fmt.Errorf("add audio transceiver failed: %v", err)
	} else if _, err = pc.pc.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo); err != nil {
		return nil, fmt.Errorf("add video transceiver failed: %v", err)
	}

	pc.pc.OnICEGatheringStateChange(func(inGathererState webrtc.ICEGathererState) {
		pc.onICEGathererStateChange(inGathererState)
	})

	pc.pc.OnICEConnectionStateChange(func(inConnectionState webrtc.ICEConnectionState) {
		pc.onICEConnectionStateChange(inConnectionState)
	})

	pc.pc.OnICECandidate(func(inCandidate *webrtc.ICECandidate) {
		pc.onICECandidate(inCandidate)
	})

	pc.pc.OnTrack(func(inTrack *webrtc.Track, inRtpReceiver *webrtc.RTPReceiver) {
		pc.onTrack(inTrack, inRtpReceiver)
	})

	return &pc, nil
}

func (pc *PeerConnection) GetId() string {
	return pc.id
}

func (pc *PeerConnection) CreateOffer(inOptions *webrtc.OfferOptions) (webrtc.SessionDescription, error) {
	sd, err := pc.pc.CreateOffer(inOptions)
	if err != nil {
		return webrtc.SessionDescription{}, err
	}
	pc.pc.SetLocalDescription(sd)
	return sd, nil
}

func (pc *PeerConnection) CreateAnswer(inOptions *webrtc.AnswerOptions) (webrtc.SessionDescription, error) {
	sd, err := pc.pc.CreateAnswer(inOptions)
	if err != nil {
		return webrtc.SessionDescription{}, err
	}
	pc.pc.SetLocalDescription(sd)
	return sd, nil
}

func (pc *PeerConnection) SetRemoteSdp(inRemoteSdp webrtc.SessionDescription) error {
	fmt.Printf("Setting remote SDP, %+v\n", inRemoteSdp)
	return pc.pc.SetRemoteDescription(inRemoteSdp)
}

func (pc *PeerConnection) onICEGathererStateChange(inGathererState webrtc.ICEGathererState) {
	fmt.Printf("ICE gatherer state %s\n", inGathererState.String())
}

func (pc *PeerConnection) onICEConnectionStateChange(inConnectionState webrtc.ICEConnectionState) {
	fmt.Printf("ICE connection state %s\n", inConnectionState.String())

	if inConnectionState == webrtc.ICEConnectionStateConnected {
		fmt.Println("ICE Connected")
	} else if inConnectionState == webrtc.ICEConnectionStateFailed ||
		inConnectionState == webrtc.ICEConnectionStateDisconnected {
	}
}

func (pc *PeerConnection) onICECandidate(inCandidate *webrtc.ICECandidate) {
	fmt.Printf("ICE candidate %v\n", inCandidate)
}

func (pc *PeerConnection) onTrack(inTrack *webrtc.Track, _ *webrtc.RTPReceiver) {
	fmt.Printf("Got track: id = %s, kind = %s, codec = %v\n",
		inTrack.ID(),
		inTrack.Kind().String(),
		*inTrack.Codec())
}
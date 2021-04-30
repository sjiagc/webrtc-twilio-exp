package recorder

import (
	"encoding/json"
	"fmt"
	"github.com/pion/webrtc/v2"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
)

type EngineState int

const (
	ESInit EngineState = 0
	ESGettingConfig EngineState = 1
	ESConfigGot EngineState = 2
	ESSignalReady EngineState = 3
	ESNegotiating EngineState = 4
	ESReady EngineState = 5
	ESError EngineState = 99

	SdpTypeOfferStr    = "offer"
	SdpTypePranswerStr = "pranswer"
	SdpTypeAnswerStr   = "answer"
	SdpTypeRollbackStr = "rollback"
)

type OnEngineErrorCallback func (error)

type Engine struct {
	token string
	onErrorCallback OnEngineErrorCallback
	state EngineState
	sc *SignalClient
	iceServers []IceServer
	pc *PeerConnection
	session string
	room *Room
}

func NewEngine(inToken string, inOnErrorCallback OnEngineErrorCallback) (*Engine, error) {
	e := Engine{}
	e.token = inToken
	e.onErrorCallback = inOnErrorCallback
	e.state = ESInit

	return &e, nil
}

func (e *Engine) Start() error {
	e.onStateChange(ESGettingConfig)
	return nil
}

func (e *Engine) Stop() {
}

func (e *Engine) onError(inErr error) {
	e.onStateChange(ESError)
	e.onErrorCallback(inErr)
}

func (e *Engine) onStateChange(inState EngineState) {
	currentState := e.state
	fmt.Printf("Engine state changed from %v to %v\n", currentState, inState)
	e.state = inState
	switch inState {
	case ESGettingConfig:
		go e.getIceConfig()
	case ESConfigGot:
		go e.startSignaling()
	case ESSignalReady:
		go e.createPeerConnection()
	case ESNegotiating:
		go e.offer()
	case ESReady:
	case ESError:
	}
}

func (e *Engine) onSignalMessage(inMsg Msg) {
	var err error
	if inMsg.GetType() != MsgTypeMsg {
		return
	}
	msg, ok := inMsg.(*MsgMsg)
	if !ok {
		fmt.Printf("Convert to MsgMsg failed")
		return
	}
	smHead := SignalMsg{}
	msg.GetBody(&smHead)
	switch smHead.Type {
	case SMsgTypeConnected: {
		smMsg := SignalMsgConnected{}
		err = msg.GetBody(&smMsg)
		if err != nil {
			fmt.Printf("Get SignalMsgConnected failed: %v\n", err)
		}
		e.onSignalMsgConnected(smMsg)
	}
	case SMsgTypeUpdate: {
		smMsg := SignalMsgUpdate{}
		err = msg.GetBody(&smMsg)
		if err != nil {
			fmt.Printf("Get SignalMsgUpdate failed: %v\n", err)
		}
		e.onSignalMsgUpdate(smMsg)
	}
	}
}

func (e *Engine) onSignalMsgConnected(inMsg SignalMsgConnected) {
	e.CreateRoom(inMsg.Sid, inMsg.Name)
	e.session = inMsg.Session
	if len(inMsg.PeerConnections) > 0 {
		localPc := e.pc
		localPcId := localPc.GetId()
		for _, pc := range inMsg.PeerConnections {
			if localPcId == pc.Id {
				sdp := webrtc.SessionDescription{}
				sdp.Type = webrtc.SDPTypeAnswer
				sdp.SDP = pc.Description.Sdp
				localPc.SetRemoteSdp(sdp)
			}
		}
	}
}

func (e *Engine) onSignalMsgUpdate(inMsg SignalMsgUpdate) {
	if len(inMsg.PeerConnections) > 0 {
		localPc := e.pc
		localPcId := localPc.GetId()
		for _, pcRecv := range inMsg.PeerConnections {
			if localPcId == pcRecv.Id {
				sdpType := pcRecv.Description.Type
				sdp := webrtc.SessionDescription{}
				sdp.Type = sdpTypeFromString(sdpType)
				sdp.SDP = pcRecv.Description.Sdp
				localPc.SetRemoteSdp(sdp)
				if sdp.Type == webrtc.SDPTypeOffer {
					answerSdp, err := localPc.CreateAnswer(nil)
					if err != nil {
						fmt.Printf("Create answer failed: %v\n", err)
					} else {
						pcSend := PeerConnectionEntity{}
						pcSend.Id = e.pc.GetId()
						pcSend.Description = PeerDescriptionEntity{
							Type: answerSdp.Type.String(),
							Revision: pcRecv.Description.Revision,
							Sdp: answerSdp.SDP,
						}
						msgUpdate, err := NewSignalMsgUpdate(e.session, 3, &pcSend)
						if err != nil {
							fmt.Printf("Create SignalMsgUpdate failed: %v\n", err)
						} else {
							msgSend, err := NewMsgMsg(msgUpdate)
							if err != nil {
								fmt.Printf("Create msg(including SignalMsgUpdate) failed: %v\n", err)
							} else {
								if err = e.sc.Send(msgSend); err != nil {
									fmt.Printf("Send msg(including SignalMsgUpdate) failed: %v\n", err)
								}
							}
						}
					}
				}
			}
		}
	}
}

func sdpTypeFromString(inSdpStr string) webrtc.SDPType {
	switch inSdpStr {
	case SdpTypeOfferStr:
		return webrtc.SDPTypeOffer
	case SdpTypePranswerStr:
		return webrtc.SDPTypePranswer
	case SdpTypeAnswerStr:
		return webrtc.SDPTypeAnswer
	case SdpTypeRollbackStr:
		return webrtc.SDPTypeRollback
	default:
		return webrtc.SDPType(999)
	}
}

func (e *Engine) onSignalError(inErrMsg string) {
	fmt.Printf("Signaling error: %s\n", inErrMsg)
}

func (e *Engine) onSignalStateChanged(inState SignalState) {
	fmt.Printf("Signaling state changed to %v\n", inState)
	if inState == SSHandshaken {
		e.onStateChange(ESSignalReady)
	}
}

var wsUrl = "wss://global.vss.twilio.com/signaling"

func (e *Engine) getIceConfig() {
	iceServers, err := getEcsConfig(e.token)
	if err != nil {
		e.onError(fmt.Errorf("Getting ICE config failed: %v\n", err))
		return
	}
	e.iceServers = iceServers
	e.onStateChange(ESConfigGot)
}

func getEcsConfig(inToken string) ([]IceServer, error) {
	type networkTraversalService struct {
		Ttl int `json:"ttl"`
		IceServers []IceServer	`json:"ice_servers"`
	}

	type video struct {
		Nts networkTraversalService	`json:"network_traversal_service"`
	}

	type repBody struct {
		V video `json:"video"`
	}

	ecsConfigUrl := "https://ecs.us1.twilio.com/v2/Configuration"

	data := url.Values{}
	data.Set("service", "video")
	data.Set("sdk_version", "2.3.0")

	client := &http.Client{}
	req, _ := http.NewRequest(http.MethodPost, ecsConfigUrl, strings.NewReader(data.Encode())) // URL-encoded payload
	req.Header.Add("x-twilio-token", inToken)

	rep, _ := client.Do(req)
	defer rep.Body.Close()

	fmt.Println(rep.Status)
	if rep.StatusCode != 200 {
		return nil, fmt.Errorf("API call failed: status = %s", rep.Status)
	}
	bodyData, err := ioutil.ReadAll(rep.Body)
	if err != nil {
		return nil, fmt.Errorf("read body failed: %v", err)
	}
	fmt.Printf("bodyData = %v\n", string(bodyData))
	body := &repBody{}
	err = json.Unmarshal(bodyData, body)
	if err != nil {
		return nil, fmt.Errorf("unmarshal json body failed: %v", err)
	}

	fmt.Printf("body = %v\n", body)

	return body.V.Nts.IceServers, nil
}

func (e *Engine) startSignaling() {
	sc, err := NewSignalClient(
		func(inMsg Msg) { e.onSignalMessage(inMsg) },
		func(inErrMsg string) { e.onSignalError(inErrMsg) },
		func(inState SignalState) { e.onSignalStateChanged(inState) })
	if err != nil {
		e.onError(fmt.Errorf("Create SignalClient failed: %v\n", err))
		return
	}
	e.sc = sc
	err = e.sc.Start()
	if err != nil {
		e.onError(fmt.Errorf("Start SignalClient failed: %v\n", err))
		return
	}
}

func (e *Engine) createPeerConnection() {
	pc, err := NewPeerConnection(e.iceServers)
	if err != nil {
		fmt.Printf("Creating PeerConnection failed: %v\n", err)
		return
	}
	e.pc = pc
	e.offer()
}

func (e *Engine) offer() {
	sdp, err := e.pc.CreateOffer(nil)
	if err != nil {
		fmt.Printf("Creating offer failed: %v\n", err)
		return
	}
	fmt.Printf("Created offer '%v'\n", sdp)

	msgPc := PeerConnectionEntity{}
	msgPc.Id = e.pc.GetId()
	msgPc.Description = PeerDescriptionEntity{}
	msgPc.Description.Type = sdp.Type.String()
	msgPc.Description.Revision = 1
	msgPc.Description.Sdp = sdp.SDP
	msgConnect, err := NewSignalMsgConnect(e.token, 3, &msgPc)

	msgMsgConnect, err := NewMsgMsg(msgConnect)

	e.sc.Send(msgMsgConnect)
}

func (e *Engine) CreateRoom(inId string, inName string) error {
	var err error
	e.room, err = NewRoom(inId, inName)
	return err
}
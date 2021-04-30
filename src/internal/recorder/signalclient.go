package recorder

import (
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"time"
)

type SignalState int

const (
	MsgTypeHello     = "hello"
	MsgTypeWelcome   = "welcome"
	MsgTypeMsg       = "msg"
	MsgTypeHeartbeat = "heartbeat"
	SMsgTypeConnect = "connect"
	SMsgTypeConnected = "connected"
	SMsgTypeUpdate = "update"

	SSInit       SignalState = 0
	SSConnected  SignalState = 1
	SSHandshaken SignalState = 2

	defaultTimeout = 5000
)

type Msg interface {
	GetType() string
}

type MsgHead struct {
	Type string	`json:"type"`
}

func (msg *MsgHead) GetType() string {
	return msg.Type
}

type MsgHello struct {
	MsgHead
	Id string		`json:"id"`
	Timeout int		`json:"timeout"`
}

func (msg *MsgHello) GetType() string {
	return msg.Type
}

type MsgWelcome struct {
	MsgHead
	NegotiatedTimeout int	`json:"negotiatedTimeout"`
}

func (msg *MsgWelcome) GetType() string {
	return msg.Type
}

type MsgMsg struct {
	MsgHead
	Body json.RawMessage `json:"body"`
}

func (msg *MsgMsg) GetType() string {
	return msg.Type
}

type MsgHeartbeat struct {
	MsgHead
}

func (msg *MsgHeartbeat) GetType() string {
	return msg.Type
}

func NewMsgHello(inId string, inTimeout int) (*MsgHello, error) {
	msg := MsgHello{}
	msg.Type = MsgTypeHello
	msg.Id = inId
	msg.Timeout = inTimeout
	return &msg, nil
}

func NewMsgMsg(inBody interface{}) (*MsgMsg, error){
	msg := MsgMsg{}
	msg.Type = MsgTypeMsg
	raw, err := json.Marshal(inBody)
	if err != nil {
		return nil, err
	}
	msg.Body = raw
	return &msg, nil
}

func NewMsgHeartbeat() (*MsgHeartbeat, error) {
	msg := MsgHeartbeat{}
	msg.Type = MsgTypeHeartbeat
	return &msg, nil
}

func (msg *MsgMsg) GetBody(inBody interface{}) error {
	return json.Unmarshal(msg.Body, inBody)
}

type OnMsgCallback func(inMsg Msg)
type OnErrorCallback func(inError string)
type OnStateChangeCallback func(inState SignalState)

type SignalClient struct {
	wsConn                *websocket.Conn
	sendChannel           chan []byte
	errChannel            chan string
	onMsgCallback         OnMsgCallback
	onErrorCallback       OnErrorCallback
	onStateChangeCallback OnStateChangeCallback
	state                 SignalState
	timeout               int
	heartbeatTicker       *time.Ticker
	heartbeatStopper      chan bool
}

func (client *SignalClient) Send(inMsg Msg) error {
	raw, err := json.Marshal(inMsg)
	if err != nil {
		return fmt.Errorf("marshal msg failed due to %v", err)
	}
	//fmt.Printf("Scheduling msg sending, %s\n", string(raw))
	client.sendChannel <- raw
	return nil
}

func (client *SignalClient) preprocessMsg(inMsg Msg) {
	if inMsg.GetType() == MsgTypeWelcome {
		msgWelcome, ok := inMsg.(*MsgWelcome)
		if !ok {
			fmt.Printf("Got invalid MsgWelcome")
			return
		}
		client.timeout = msgWelcome.NegotiatedTimeout
		client.transitState(SSHandshaken)
	}
}

func (client *SignalClient) kickOffHeartbeat() {
	ticker := time.NewTicker(time.Duration(client.timeout) * time.Millisecond)
	done := make(chan bool)
	go func() {
		for {
			select {
			case <-done:
				return
			case t := <-ticker.C:
				msgHeartbeat, err := NewMsgHeartbeat()
				if err != nil {
					fmt.Printf("Create MsgHeartbeat failed, %v\n", err)
					break
				}
				client.Send(msgHeartbeat)
				fmt.Println("Sent heartbeat at\n", t)
			}
		}
	}()
	client.heartbeatTicker = ticker
	client.heartbeatStopper = done
}

func (client *SignalClient) transitState(inState SignalState) {
	currentState := client.state
	switch currentState {
	case SSInit:
		if inState != SSConnected {
			fmt.Printf("Transit from state %v to %v is illegal\n", currentState, inState)
			return
		}
		client.state = inState
	case SSConnected:
		if inState != SSHandshaken {
			fmt.Printf("Transit from state %v to %v is illegal\n", currentState, inState)
			return
		}
		client.state = inState
		client.kickOffHeartbeat()
	}
	if client.onStateChangeCallback != nil {
		client.onStateChangeCallback(inState)
	}
}

func (client *SignalClient) sendFromQueue() {
	for {
		fmt.Printf("Waiting for msg to send\n")
		raw, ok := <-client.sendChannel
		if !ok {
			fmt.Println("send channel closed, quit")
			break
		}
		fmt.Printf("sending msg %s\n", string(raw))
		if err := client.wsConn.WriteMessage(websocket.TextMessage, raw); err != nil {
			fmt.Printf("send: Write msg failed, error = %v\n", err)
			continue
		}
	}
}

func (client *SignalClient) recvToQueue() {
	for {
		msgType, raw, err := client.wsConn.ReadMessage()
		if err != nil {
			fmt.Printf("recv: Read msg failed, error = %v\n", err)
			client.errChannel <- "read-msg-failed"
			break
		}
		if msgType != websocket.TextMessage {
			fmt.Printf("recv: Invalid msg type %d\n", msgType)
			continue
		}
		fmt.Printf("recvToQueue: Got msg %s\n", string(raw))
		msgHead := MsgHead{}
		err = json.Unmarshal(raw, &msgHead)
		if err != nil {
			fmt.Printf("recv: Unmarshal MsgHead failed, error = %v\n", err)
			continue
		}
		var msg Msg = nil
		switch msgHead.Type {
		case MsgTypeHello:
			msgHello := MsgHello{}
			if err = json.Unmarshal(raw, &msgHello); err != nil {
				fmt.Printf("recv Unmarshal MsgHello failed, error = %v\n", err)
				break
			}
			msg = &msgHello
		case MsgTypeWelcome:
			msgWelcome := MsgWelcome{}
			if err = json.Unmarshal(raw, &msgWelcome); err != nil {
				fmt.Printf("recv Unmarshal MsgWelcome failed, error = %v\n", err)
				break
			}
			msg = &msgWelcome
		case MsgTypeMsg:
			msgMsg := MsgMsg{}
			if err = json.Unmarshal(raw, &msgMsg); err != nil {
				fmt.Printf("recv Unmarshal MsgMsg failed, error = %v\n", err)
				break
			}
			msg = &msgMsg
		case MsgTypeHeartbeat:
			msgHeartbeat := MsgHeartbeat{}
			if err = json.Unmarshal(raw, &msgHeartbeat); err != nil {
				fmt.Printf("recv Unmarshal MsgHeartbeat failed, error = %v\n", err)
				break
			}
			msg = &msgHeartbeat
		}
		if msg == nil {
			continue
		}
		client.preprocessMsg(msg)
		if client.onMsgCallback != nil {
			client.onMsgCallback(msg)
		}
	}
}

func (client *SignalClient) Start() error {
	msgHello, err := NewMsgHello(uuid.New().String(), defaultTimeout)
	if err != nil {
		return fmt.Errorf("create MsgHello failed: %v", err)
	}
	client.Send(msgHello)
	return nil
}

func NewSignalClient(inOnMsgCallback OnMsgCallback,
					 inOnErrCallback OnErrorCallback,
					 inOnStateChangeCallback OnStateChangeCallback) (*SignalClient, error) {
	client := SignalClient{}
	client.state = SSInit
	client.onMsgCallback = inOnMsgCallback
	client.onErrorCallback = inOnErrCallback
	client.onStateChangeCallback = inOnStateChangeCallback
	url := "wss://global.vss.twilio.com/signaling"
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		return nil, fmt.Errorf("WebSocket dailing failed: %v", err)
	}
	client.wsConn = conn
	client.sendChannel = make(chan []byte, 100)
	client.errChannel = make(chan string, 10)
	go client.sendFromQueue()
	go client.recvToQueue()

	client.transitState(SSConnected)
	return &client, nil
}

type TrackEntity struct {
	Enabled  bool   `json:"enabled"`
	Id       string `json:"id"`
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	Priority string `json:"priority"`
	Sid      string `json:"sid,omitempty"`
	State    string `json:"state"`
}

type ParticipantEntity struct {
	Sid      string        `json:"sid,omitempty"`
	Identity string        `json:"identity,omitempty"`
	State    string        `json:"state,omitempty"`
	Revision int           `json:"revision"`
	Tracks   []TrackEntity `json:"tracks"`
}

type PeerDescriptionEntity struct {
	Type string			`json:"type"`
	Revision int		`json:"revision"`
	Sdp string			`json:"sdp"`
}

type PeerConnectionEntity struct {
	Id          string                `json:"id"`
	Description PeerDescriptionEntity `json:"description"`
}

type PublisherEntity struct {
	Name       string `json:"name"`
	SdkVersion string `json:"sdk_version"`
	UserAgent  string `json:"user_agent"`
}

type TransportEntity struct {
	Type string `json:"type"`
}

type TrackPriorityEntity struct {
	Transports []TransportEntity `json:"transports"`
}

type TrackSwitchOffEntity struct {
	Transports []TransportEntity `json:"transports"`
}

type MediaSignalingEntity struct {
	TrackPriority  TrackPriorityEntity  `json:"track_priority"`
	TrackSwitchOff TrackSwitchOffEntity `json:"track_switch_off"`
}

type SubscribeRuleEntity struct {
	Type string `json:"type"`
	All bool `json:"all"`
}

type SubscribeEntity struct {
	Rules    []SubscribeRuleEntity `json:"rules"`
	Revision int                   `json:"revision"`
}

type RecordingEntity struct {
	Enabled  bool `json:"enabled"`
	Revision int  `json:"revision"`
}

type SubscribedTrackEntity struct {
	Id  string `json:"id"`
	Sid string `json:"sid,omitempty"`
}

type SubscribedEntity struct {
	Revision int                     `json:"revision"`
	Tracks   []SubscribedTrackEntity `json:"tracks"`
}

type PublishedEntity struct {
	Revision int           `json:"revision"`
	Tracks   []TrackEntity `json:"tracks"`
}

type RoomOptionsEntity struct {
	MediaRegion     string `json:"media_region"`
	SignalingRegion string `json:"signaling_region"`
	SessionTimeout  int    `json:"session_timeout"`
	SignalingEdge   string `json:"signaling_edge"`
}

type SignalMsg struct {
	Type    string `json:"type"`
	Version int    `json:"version"`
}

type SignalMsgConnect struct {
	SignalMsg
	Name            *string                `json:"name"`
	Participant     ParticipantEntity      `json:"participant"`
	PeerConnections []PeerConnectionEntity `json:"peer_connections"`
	IceServers      string                 `json:"ice_servers"`
	Publisher       PublisherEntity        `json:"publisher"`
	MediaSignaling  *MediaSignalingEntity  `json:"media_signaling,omitempty"`
	Subscribe       SubscribeEntity        `json:"subscribe"`
	Format          string                 `json:"format"`
	Token           string                 `json:"token"`
}

func NewSignalMsgConnect(inToken string,
						 inParticipantRevision int,
						 inPeerConnection *PeerConnectionEntity) (*SignalMsgConnect, error) {
	msg := SignalMsgConnect{}
	msg.Type = SMsgTypeConnect
	msg.Version = 2
	msg.Name = nil
	msg.Participant = ParticipantEntity{}
	msg.Participant.Revision = inParticipantRevision
	msg.Participant.Tracks = []TrackEntity{}
	msg.PeerConnections = append(msg.PeerConnections, *inPeerConnection)
	msg.IceServers = "success"
	publisher := PublisherEntity{}
	publisher.Name = "twilio-video.js"
	publisher.SdkVersion = "2.3.0"
	publisher.UserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/80.0.3987.163 Safari/537.36"
	msg.Publisher = publisher
	msg.MediaSignaling = nil
	//msg.MediaSignalingEntity = MediaSignalingEntity{}
	//msg.MediaSignalingEntity.TrackPriorityEntity = TrackPriorityEntity{}
	//msg.MediaSignalingEntity.TrackPriorityEntity.Transports = []TransportEntity{}
	//msg.MediaSignalingEntity.TrackSwitchOffEntity = TrackSwitchOffEntity{}
	//msg.MediaSignalingEntity.TrackSwitchOffEntity.Transports = []TransportEntity{}
	subscribe := SubscribeEntity{}
	subscribe.Revision = 1
	subscribeRule := SubscribeRuleEntity{}
	subscribeRule.Type = "include"
	subscribeRule.All = true
	subscribe.Rules = append(subscribe.Rules, subscribeRule)
	msg.Format = "unified"
	msg.Token = inToken

	return &msg, nil
}

type SignalMsgConnected struct {
	SignalMsg
	Sid             string                 `json:"sid"`
	Name            string                 `json:"name"`
	Session         string                 `json:"session"`
	Participant     ParticipantEntity      `json:"participant"`
	Participants    []ParticipantEntity    `json:"participants"`
	PeerConnections []PeerConnectionEntity `json:"peer_connections"`
	Recording       RecordingEntity        `json:"recording"`
	Subscribed      SubscribedEntity       `json:"subscribed"`
	Published       PublishedEntity        `json:"published"`
	MediaSignaling  MediaSignalingEntity   `json:"media_signaling,omitempty"`
	Options         RoomOptionsEntity      `json:"options"`
}

type SignalMsgUpdate struct {
	SignalMsg
	Sid             string                 `json:"sid,omitempty"`
	Name            string                 `json:"name,omitempty"`
	Session         string                 `json:"session,omitempty"`
	Participant     *ParticipantEntity      `json:"participant,omitempty"`
	Participants    []ParticipantEntity    `json:"participants,omitempty"`
	PeerConnections []PeerConnectionEntity `json:"peer_connections,omitempty"`
	Recording       *RecordingEntity        `json:"recording,omitempty"`
	Subscribe       *SubscribeEntity        `json:"subscribe,omitempty"`
	Subscribed      *SubscribedEntity       `json:"subscribed,omitempty"`
	Published       *PublishedEntity        `json:"published,omitempty"`
}

func NewSignalMsgUpdate(inSession string,
						inParticipantRevision int,
						inPeerConnection *PeerConnectionEntity) (*SignalMsgUpdate, error) {
	msg := SignalMsgUpdate{}
	msg.Type = SMsgTypeUpdate
	msg.Version = 2
	msg.Sid = ""
	msg.Name = ""
	msg.Session = inSession
	msg.Participant = &ParticipantEntity{}
	msg.Participant.Revision = inParticipantRevision
	msg.Participant.Tracks = []TrackEntity{}
	if inPeerConnection != nil {
		msg.PeerConnections = append(msg.PeerConnections, *inPeerConnection)
	}

	return &msg, nil
}

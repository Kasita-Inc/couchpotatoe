package loxone

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/gorilla/websocket"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const (
	textMessage           = 0
	binaryFile            = 1
	valueEvent            = 2
	textEvent             = 3
	daytimerEvent         = 4
	outOfServiceIndicator = 5
	keepAlive             = 6
	weatherEvent          = 7
)

type payload struct {
	cmd  string
	err  error
	data interface{}
}

type WebSocket struct {
	conn  *websocket.Conn
	queue chan payload
}

// Connect connects the WebSocket to the Miniserver.
func Connect(host string) (socket *WebSocket, err error) {
	websocketURL := url.URL{Scheme: "ws", Host: host, Path: "/ws/rfc6455"}
	protoHeaders := http.Header{"Sec-WebSocket-Protocol": {"remotecontrol"}}
	conn, _, err := websocket.DefaultDialer.Dial(websocketURL.String(), protoHeaders)
	if err == nil {
		socket = &WebSocket{conn, make(chan payload)}
		go socket.processIncomingMessages()
	}
	return socket, err
}

// Authenticate authenticates the connection with the given credentials.
func (socket *WebSocket) Authenticate(username, password string) (err error) {
	val, err := socket.call("jdev/sys/getkey")
	if err == nil {
		key, err := hex.DecodeString(val.(string))
		if err == nil {
			cred := []byte(fmt.Sprintf("%s:%s", username, password))
			comp := hmac.New(sha1.New, key)
			comp.Write(cred)
			hash := hex.EncodeToString(comp.Sum(nil))
			_, err = socket.call(fmt.Sprintf("authenticate/%s", hash))
		}
	}
	return err
}

// LoxAPP3 returns the Miniserver structure file.
func (socket *WebSocket) LoxAPP3() (app3 map[string]interface{}, err error) {
	data, err := socket.call("data/LoxApp3.json")
	if err == nil {
		json.Unmarshal(data.([]byte), &app3)
	}
	return app3, err
}

// EnableStatusUpdate enables the Miniserver to push status update notifications.
func (socket *WebSocket) EnableStatusUpdate() (err error) {
	_, err = socket.call("jdev/sps/enablebinstatusupdate")
	return err
}

func (socket *WebSocket) call(cmd string) (val interface{}, err error) {
	err = socket.conn.WriteMessage(websocket.TextMessage, []byte(cmd))
	if err == nil {
		p := <-socket.queue
		err = p.err
		if err == nil {
			if strings.HasPrefix(cmd, "data") || strings.HasSuffix(cmd, p.cmd) {
				val = p.data
			} else {
				err = fmt.Errorf("response payload does not match command (%s != %s)", cmd, p.cmd)
			}
		}
	}
	return val, err
}

func (socket *WebSocket) processIncomingMessages() {
	for {
		msgType, msgData, err := socket.readMessage()
		if err != nil {
			panic(err)
		}

		switch msgType {
		case textMessage:
			cmd, val, err := decodeMsgText(msgData)
			if err != nil {
				panic(err)
			}
			socket.queue <- payload{cmd, err, val}
		case binaryFile:
			socket.queue <- payload{"", nil, msgData}
		default:
			panic(fmt.Errorf("unhandled message type %d", msgType))
		}
	}
}

func (socket *WebSocket) readMessage() (msgType uint8, msgData []byte, err error) {
	sockMsgType, header, err := socket.conn.ReadMessage()
	if err == nil {
		if sockMsgType != websocket.BinaryMessage {
			err = fmt.Errorf("invalid message type")
		} else {
			var msgSize uint32
			msgType, msgSize, err = decodeMsgHeader(header)
			if err == nil {
				sockMsgType, msgData, err = socket.conn.ReadMessage()
				if isBinaryTextMessage(msgType, msgData) {
					header = msgData
					_, msgSize, err = decodeMsgHeader(header)
					_, msgData, err = socket.conn.ReadMessage()
				}
				if err == nil {
					if len(msgData) != int(msgSize) {
						err = fmt.Errorf("invalid message size")
					} else if sockMsgType == websocket.TextMessage && msgType != textMessage {
						err = fmt.Errorf("invalid message type")
					}
				}
			}
		}
	}
	return msgType, msgData, err
}

func isBinaryTextMessage(msgType uint8, data []byte) bool {
	return msgType == binaryFile && len(data) == 8 && data[0] == 0x03
}

func decodeMsgHeader(msg []byte) (identifier uint8, length uint32, err error) {
	if len(msg) != 8 {
		err = fmt.Errorf("invalid message header length")
	} else if msg[0] != 0x03 {
		err = fmt.Errorf("invalid message header first byte")
	} else {
		identifier = msg[1]
		length = binary.LittleEndian.Uint32(msg[4:])
	}
	return identifier, length, err
}

func decodeMsgText(msg []byte) (cmd string, val interface{}, err error) {
	var resp map[string]interface{}
	err = json.Unmarshal(msg, &resp)
	if err == nil {
		data := resp["LL"].(map[string]interface{})
		code, err := strconv.Atoi(data["Code"].(string))
		if err == nil {
			if code != 200 {
				err = fmt.Errorf("invalid response status code")
			} else {
				cmd = data["control"].(string)
				val = data["value"]
			}
		}
	}
	return cmd, val, err
}

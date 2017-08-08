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
)

var addr = "192.168.2.59:80"

const (
	textMessage = 0
	binaryFile  = 1
)

type WebSocket struct {
	conn *websocket.Conn
}

// Connect connects the WebSocket to the Miniserver.
func Connect() (socket *WebSocket, err error) {
	websocketURL := url.URL{Scheme: "ws", Host: addr, Path: "/ws/rfc6455"}
	protoHeaders := http.Header{"Sec-WebSocket-Protocol": {"remotecontrol"}}
	conn, _, err := websocket.DefaultDialer.Dial(websocketURL.String(), protoHeaders)
	return &WebSocket{conn}, err
}

// Authenticate authenticates the connection with the given credentials.
func (socket *WebSocket) Authenticate(username, password string) (err error) {
	err = socket.conn.WriteMessage(websocket.TextMessage, []byte("jdev/sys/getkey"))
	if err == nil {
		_, msgData, err := socket.readMessage()
		if err == nil {
			val, err := decodeMsgText(msgData)
			if err == nil {
				key, err := hex.DecodeString(val.(string))
				if err == nil {
					cred := []byte(fmt.Sprintf("%s:%s", username, password))
					comp := hmac.New(sha1.New, key)
					comp.Write(cred)
					hash := hex.EncodeToString(comp.Sum(nil))
					err = socket.conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("authenticate/%s", hash)))
					if err == nil {
						_, msgData, err := socket.readMessage()
						if err == nil {
							_, err = decodeMsgText(msgData)
						}
					}
				}
			}
		}
	}
	return err
}

// LoxAPP3 returns the Miniserver structure file.
func (socket *WebSocket) LoxAPP3() (app3 map[string]interface{}, err error) {
	err = socket.conn.WriteMessage(websocket.TextMessage, []byte("data/LoxAPP3.json"))
	if err == nil {
		_, data, err := socket.readMessage()
		if err == nil {
			err = json.Unmarshal(data, &app3)
		}
	}
	return app3, err
}

// EnableStatusUpdate enables the Miniserver to push status update notifications.
func (socket *WebSocket) EnableStatusUpdate() (err error) {
	err = socket.conn.WriteMessage(websocket.TextMessage, []byte("jdev/sps/enablebinstatusupdate"))
	if err == nil {
		_, msgData, err := socket.readMessage()
		if err == nil {
			_, err = decodeMsgText(msgData)
		}
	}
	return err
}

func (socket *WebSocket) readMessage() (msgType uint8, msgData []byte, err error) {
	sockMsgType, data, err := socket.conn.ReadMessage()

	if err == nil {
		if sockMsgType != websocket.BinaryMessage {
			err = fmt.Errorf("invalid message type")
		} else {
			msgType, msgSize, err := decodeMsgHeader(data)
			if err == nil {
				if msgType == binaryFile {
					// Skip the first message header.
					// Not sure why we receive two headers thought?!
					socket.conn.ReadMessage()
				}
				sockMsgType, msgData, err = socket.conn.ReadMessage()
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

func decodeMsgText(msg []byte) (val interface{}, err error) {
	var resp map[string]interface{}
	err = json.Unmarshal(msg, &resp)
	if err == nil {
		data := resp["LL"].(map[string]interface{})
		code, err := strconv.Atoi(data["Code"].(string))
		if err == nil {
			if code != 200 {
				err = fmt.Errorf("invalid response status code")
			} else {
				val = data["value"]
			}
		}
	}
	return val, err
}

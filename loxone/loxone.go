package loxone

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/gorilla/websocket"
	gouuid "github.com/satori/go.uuid"
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

type DayTimerEntry struct {
	mode, from, to, needActivate int32
	val                          float64
}

type DayTimerEvent struct {
	defaultVal float64
	entries    []DayTimerEntry
}

type WeatherEntry struct {
	timestamp, weatherType, windDirection, solarRadiation, relativeHumidity                  int32
	temperature, perceivedTemperature, dewPoint, pricipitation, windSpeed, barometicPressure float64
}

type WeatherEvent struct {
	lastUpdate uint32
	entries    []WeatherEntry
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
		case valueEvent:
			decodeValueEventTable(msgData)
		case textEvent:
			decodeTextEventTable(msgData)
		case daytimerEvent:
			decodeDaytimerEventTable(msgData)
		case weatherEvent:
			decodeWeatherEventTable(msgData)
		default:
			fmt.Println("ignoring message type", msgType)
			//panic(fmt.Errorf("unhandled message type %d", msgType))
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

func decodeValueEvent(msg []byte) (uuid gouuid.UUID, val float64, err error) {
	if len(msg) != 24 {
		err = fmt.Errorf("invalid value event message length")
	} else {
		uuid, err = gouuid.FromBytes(msg[:16])
		if err == nil {
			err = binary.Read(bytes.NewReader(msg[16:24]), binary.LittleEndian, &val)
		}
	}
	return uuid, val, err
}

func decodeValueEventTable(msg []byte) (table map[gouuid.UUID]float64, err error) {
	table = make(map[gouuid.UUID]float64)
	for i := 0; i < len(msg); i += 24 {
		uuid, val, err := decodeValueEvent(msg[i : i+24])
		if err != nil {
			break
		}
		table[uuid] = val
	}
	return table, err
}

func decodeTextEvent(msg []byte) (uuid, uuidIcon gouuid.UUID, text string, err error) {
	uuid, err = gouuid.FromBytes(msg[:16])
	if err == nil {
		uuidIcon, err = gouuid.FromBytes(msg[16:32])
		if err == nil {
			textLength := binary.LittleEndian.Uint32(msg[32:36])
			text = string(msg[36 : 36+textLength])
		}
	}
	return uuid, uuidIcon, text, err
}

func decodeTextEventTable(msg []byte) (table map[gouuid.UUID]string, err error) {
	table = make(map[gouuid.UUID]string)
	for i := 0; i < len(msg); {
		uuid, _, text, err := decodeTextEvent(msg[i:])
		if err != nil {
			break
		}
		table[uuid] = text
		textLength := len(text)
		i += textLength + textLength%4 + 36
	}
	return table, err
}

func decodeDaytimerEntry(msg []byte) (entry DayTimerEntry, err error) {
	if len(msg) != 24 {
		err = fmt.Errorf("invalid daytimer entry message length")
	} else {
		if err = binary.Read(bytes.NewReader(msg[0:4]), binary.LittleEndian, &entry.mode); err == nil {
			if err = binary.Read(bytes.NewReader(msg[4:8]), binary.LittleEndian, &entry.from); err == nil {
				if err = binary.Read(bytes.NewReader(msg[8:12]), binary.LittleEndian, &entry.to); err == nil {
					if err = binary.Read(bytes.NewReader(msg[12:16]), binary.LittleEndian, &entry.needActivate); err == nil {
						err = binary.Read(bytes.NewReader(msg[16:24]), binary.LittleEndian, &entry.val)
					}
				}
			}
		}
	}
	return entry, err
}

func decodeDaytimerEventTable(msg []byte) (table map[gouuid.UUID]DayTimerEvent, err error) {
	table = make(map[gouuid.UUID]DayTimerEvent)
	for i := 0; i < len(msg); {
		uuid, err := gouuid.FromBytes(msg[i : i+16])
		if err != nil {
			break
		}
		var defaultVal float64
		if err = binary.Read(bytes.NewReader(msg[i+16:i+24]), binary.LittleEndian, &defaultVal); err != nil {
			break
		}
		var nrEntries int32
		if err = binary.Read(bytes.NewReader(msg[i+24:i+28]), binary.LittleEndian, &nrEntries); err != nil {
			break
		}
		entries := make([]DayTimerEntry, nrEntries)
		i += 28
		max := i + int(nrEntries)*24
		for i < max {
			entry, err := decodeDaytimerEntry(msg[i : i+24])
			if err != nil {
				break
			}
			entries = append(entries, entry)
			i += 24
		}
		if err != nil {
			break
		}
		table[uuid] = DayTimerEvent{defaultVal, entries}
	}
	return table, err
}

func decodeWeatherEntry(msg []byte) (entry WeatherEntry, err error) {
	if len(msg) != 68 {
		err = fmt.Errorf("invalid weather entry message length")
	} else {
		if err = binary.Read(bytes.NewReader(msg[:4]), binary.LittleEndian, &entry.timestamp); err == nil {
			if err = binary.Read(bytes.NewReader(msg[4:8]), binary.LittleEndian, &entry.weatherType); err == nil {
				if err = binary.Read(bytes.NewReader(msg[8:12]), binary.LittleEndian, &entry.windDirection); err == nil {
					if err = binary.Read(bytes.NewReader(msg[12:16]), binary.LittleEndian, &entry.solarRadiation); err == nil {
						if err = binary.Read(bytes.NewReader(msg[16:20]), binary.LittleEndian, &entry.relativeHumidity); err == nil {
							if err = binary.Read(bytes.NewReader(msg[20:28]), binary.LittleEndian, &entry.temperature); err == nil {
								if err = binary.Read(bytes.NewReader(msg[28:36]), binary.LittleEndian, &entry.perceivedTemperature); err == nil {
									if err = binary.Read(bytes.NewReader(msg[36:44]), binary.LittleEndian, &entry.dewPoint); err == nil {
										if err = binary.Read(bytes.NewReader(msg[44:52]), binary.LittleEndian, &entry.pricipitation); err == nil {
											if err = binary.Read(bytes.NewReader(msg[52:60]), binary.LittleEndian, &entry.windSpeed); err == nil {
												err = binary.Read(bytes.NewReader(msg[60:68]), binary.LittleEndian, &entry.barometicPressure)
											}
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}
	return entry, err
}

func decodeWeatherEventTable(msg []byte) (table map[gouuid.UUID]WeatherEvent, err error) {
	table = make(map[gouuid.UUID]WeatherEvent)
	for i := 0; i < len(msg); {
		uuid, err := gouuid.FromBytes(msg[i : i+16])
		if err != nil {
			break
		}
		var lastUpdate uint32
		if err = binary.Read(bytes.NewReader(msg[i+16:i+20]), binary.LittleEndian, &lastUpdate); err != nil {
			break
		}
		var nrEntries int32
		if err = binary.Read(bytes.NewReader(msg[i+20:i+24]), binary.LittleEndian, &nrEntries); err != nil {
			break
		}
		entries := make([]WeatherEntry, nrEntries)
		i += 24
		max := i + int(nrEntries)*68
		for i < max {
			entry, err := decodeWeatherEntry(msg[i : i+68])
			if err != nil {
				break
			}
			entries = append(entries, entry)
			i += 68
		}
		if err != nil {
			break
		}
		table[uuid] = WeatherEvent{lastUpdate, entries}
	}
	return table, err
}

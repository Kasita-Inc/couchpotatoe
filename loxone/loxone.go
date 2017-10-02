package loxone

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/cskr/pubsub"
	"github.com/gorilla/websocket"
	"log"
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

type UUID = string

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

var broker = pubsub.New(1)

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

// Subscribe returns a channel for receiving update notifications for a given uuid.
func (socket *WebSocket) Subscribe(uuid UUID) chan interface{} {
	return broker.Sub(uuid)
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
			log.Println(err)
		}

		switch msgType {
		case textMessage:
			cmd, val, err := decodeMsgText(msgData)
			if err != nil {
				log.Println(err)
			}
			socket.queue <- payload{cmd, err, val}
		case binaryFile:
			socket.queue <- payload{"", nil, msgData}
		case valueEvent:
			if t, err := decodeValueEventTable(msgData); err == nil {
				socket.publishEventTable(t, msgType)
			} else {
				log.Println(err)
			}
		case textEvent:
			if t, err := decodeTextEventTable(msgData); err == nil {
				socket.publishEventTable(t, msgType)
			} else {
				log.Println(err)
			}
		case daytimerEvent:
			if t, err := decodeDaytimerEventTable(msgData); err == nil {
				socket.publishEventTable(t, msgType)
			} else {
				log.Println(err)
			}
		case weatherEvent:
			if t, err := decodeWeatherEventTable(msgData); err == nil {
				socket.publishEventTable(t, msgType)
			} else {
				log.Println(err)
			}
		default:
			log.Println(fmt.Errorf("unhandled message type %d", msgType))
		}
	}
}

func (socket *WebSocket) publishEventTable(events map[UUID]interface{}, eventType uint8) {
	for k, v := range events {
		broker.Pub(v, k)
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

func decodeUUID(msg []byte) (uuid UUID, err error) {
	if len(msg) != 16 {
		err = fmt.Errorf("invalid uuid length")
	} else {
		var data1 uint32
		var data2, data3 uint16
		var data4 [8]byte
		if err = binary.Read(bytes.NewReader(msg[0:4]), binary.LittleEndian, &data1); err == nil {
			if err = binary.Read(bytes.NewReader(msg[4:6]), binary.LittleEndian, &data2); err == nil {
				if err = binary.Read(bytes.NewReader(msg[6:8]), binary.LittleEndian, &data3); err == nil {
					if err = binary.Read(bytes.NewReader(msg[8:16]), binary.LittleEndian, &data4); err == nil {
						uuid = fmt.Sprintf("%08x-%04x-%04x-%02x%02x%02x%02x%02x%02x%02x%02x", data1, data2, data3, data4[0], data4[1], data4[2], data4[3], data4[4], data4[5], data4[6], data4[7])
					}
				}
			}
		}
	}
	return uuid, err
}

func decodeValueEvent(msg []byte) (uuid UUID, val float64, err error) {
	if len(msg) != 24 {
		err = fmt.Errorf("invalid value event length")
	} else {
		uuid, err = decodeUUID(msg[:16])
		if err == nil {
			err = binary.Read(bytes.NewReader(msg[16:24]), binary.LittleEndian, &val)
		}
	}
	return uuid, val, err
}

func decodeValueEventTable(msg []byte) (table map[UUID]interface{}, err error) {
	table = make(map[UUID]interface{})
	for i := 0; i < len(msg); i += 24 {
		uuid, val, err := decodeValueEvent(msg[i : i+24])
		if err != nil {
			return table, err
		}
		table[uuid] = val
	}
	return table, err
}

func decodeTextEvent(msg []byte) (uuid, uuidIcon UUID, text string, err error) {
	uuid, err = decodeUUID(msg[:16])
	if err == nil {
		uuidIcon, err = decodeUUID(msg[16:32])
		if err == nil {
			textLength := binary.LittleEndian.Uint32(msg[32:36])
			if int(textLength) > len(msg)-36 {
				text = string(msg[36:])
				err = fmt.Errorf("invalid text event with length %d", textLength)
			} else {
				text = string(msg[36 : 36+textLength])
			}
		}
	}
	return uuid, uuidIcon, text, err
}

func decodeTextEventTable(msg []byte) (table map[UUID]interface{}, err error) {
	table = make(map[UUID]interface{})
	for i := 0; i < len(msg); {
		uuid, _, text, err := decodeTextEvent(msg[i:])
		if err != nil {
			return table, err
		}
		table[uuid] = text
		textLength := len(text)
		i += textLength + textLength%4 + 36
	}
	return table, err
}

func decodeDaytimerEntry(msg []byte) (entry DayTimerEntry, err error) {
	if len(msg) != 24 {
		err = fmt.Errorf("invalid daytimer entry length")
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

func decodeDaytimerEventTable(msg []byte) (table map[UUID]interface{}, err error) {
	table = make(map[UUID]interface{})
	for i := 0; i < len(msg); {
		uuid, err := decodeUUID(msg[i : i+16])
		if err != nil {
			return table, err
		}
		var defaultVal float64
		if err = binary.Read(bytes.NewReader(msg[i+16:i+24]), binary.LittleEndian, &defaultVal); err != nil {
			return table, err
		}
		var nrEntries int32
		if err = binary.Read(bytes.NewReader(msg[i+24:i+28]), binary.LittleEndian, &nrEntries); err != nil {
			return table, err
		}
		entries := make([]DayTimerEntry, nrEntries)
		i += 28
		max := i + int(nrEntries)*24
		for i < max {
			entry, err := decodeDaytimerEntry(msg[i : i+24])
			if err != nil {
				return table, err
			}
			entries = append(entries, entry)
			i += 24
		}
		if err != nil {
			return table, err
		}
		table[uuid] = DayTimerEvent{defaultVal, entries}
	}
	return table, err
}

func decodeWeatherEntry(msg []byte) (entry WeatherEntry, err error) {
	if len(msg) != 68 {
		err = fmt.Errorf("invalid weather entry length")
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

func decodeWeatherEventTable(msg []byte) (table map[UUID]interface{}, err error) {
	table = make(map[UUID]interface{})
	for i := 0; i < len(msg); {
		uuid, err := decodeUUID(msg[i : i+16])
		if err != nil {
			return table, err
		}
		var lastUpdate uint32
		if err = binary.Read(bytes.NewReader(msg[i+16:i+20]), binary.LittleEndian, &lastUpdate); err != nil {
			return table, err
		}
		var nrEntries int32
		if err = binary.Read(bytes.NewReader(msg[i+20:i+24]), binary.LittleEndian, &nrEntries); err != nil {
			return table, err
		}
		entries := make([]WeatherEntry, nrEntries)
		i += 24
		max := i + int(nrEntries)*68
		for i < max {
			entry, err := decodeWeatherEntry(msg[i : i+68])
			if err != nil {
				return table, err
			}
			entries = append(entries, entry)
			i += 68
		}
		if err != nil {
			return table, err
		}
		table[uuid] = WeatherEvent{lastUpdate, entries}
	}
	return table, err
}

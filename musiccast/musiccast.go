package musiccast

import (
	"encoding/json"
	"fmt"
	upnp "github.com/huin/goupnp"
	"github.com/huin/goupnp/dcps/av1"
	"github.com/kr/pretty"
	"log"
	"net"
	"net/http"
	"net/url"
	"path"
	"sync"
)

type event map[string]interface{}

type status struct {
	Input     string `json:"input"`
	Power     string `json:"power"`
	Sleep     uint8  `json:"sleep"`
	Volume    uint8  `json:"volume"`
	Mute      bool   `json:"mute"`
	MaxVolume uint8  `json:"max_volume"`
}

type playback struct {
	Input       string `json:"input"`
	Playback    string `json:"playback"`
	Repeat      string `json:"repeat"`
	Shuffle     string `json:"shuffle"`
	PlayTime    int32  `json:"play_time"`
	TotalTime   int32  `json:"total_time"`
	Artist      string `json:"artist"`
	Album       string `json:"album"`
	AlbumArtURL string `json:"albumart_url"`
	Track       string `json:"track"`
}

type Device struct {
	id, model, name        string
	status                 status
	playback               playback
	extendedControlBaseURL url.URL
	httpClient             *http.Client
	avTransport            *av1.AVTransport1
	mutex                  *sync.RWMutex
}

var availableDevices = make(map[string]*Device)

// Discover attempts to find MusicCast devices on the local network.
func Discover() (devices []*Device, err error) {
	maybeRootDevices, err := upnp.DiscoverDevices("urn:schemas-upnp-org:device:MediaRenderer:1")
	if err == nil {
		for _, maybeRoot := range maybeRootDevices {
			d, err := NewDevice(maybeRoot)
			if err == nil {
				availableDevices[d.id] = d
				devices = append(devices, d)
			}
		}
	}

	return devices, err
}

// Listen listens and dispatches incoming YXC events.
func Listen() {
	go func() {
		listenAddr, err := net.ResolveUDPAddr("udp", ":41100")
		if err != nil {
			panic(err)
		}

		conn, err := net.ListenUDP("udp", listenAddr)
		if err != nil {
			panic(err)
		}

		buf := make([]byte, 1024)
		defer conn.Close()

		for {
			size, _, err := conn.ReadFromUDP(buf)
			if err != nil {
				panic(err)
			}
			var payload event
			err = json.Unmarshal(buf[0:size], &payload)
			if err != nil {
				panic(err)
			}
			d := availableDevices[payload["device_id"].(string)]
			err = d.processEvent(payload)
			if err != nil {
				panic(err)
			}
		}
	}()
}

// NewDevice creates a new Device from the given UPnP root device.
func NewDevice(maybeRoot upnp.MaybeRootDevice) (device *Device, err error) {
	err = maybeRoot.Err
	if err == nil {
		extendedControlURL := maybeRoot.Root.Device.PresentationURL.URL
		extendedControlURL.Path = path.Join(extendedControlURL.Path, "YamahaExtendedControl", "v1")
		avTransportClients, err := av1.NewAVTransport1ClientsFromRootDevice(maybeRoot.Root, maybeRoot.Location)
		if err == nil {
			device = &Device{"", "", "", status{}, playback{}, extendedControlURL, &http.Client{}, avTransportClients[0], &sync.RWMutex{}}
			err = device.sync()
		}
	}

	return device, err
}

// GetDeviceID returns the device id.
func (d *Device) GetDeviceID() string {
	d.mutex.RLock()
	defer d.mutex.RUnlock()
	return d.id
}

// GetDeviceModel returns the device model.
func (d *Device) GetDeviceModel() string {
	d.mutex.RLock()
	defer d.mutex.RUnlock()
	return d.model
}

// GetNetworkName returns the device network name.
func (d *Device) GetNetworkName() string {
	d.mutex.RLock()
	defer d.mutex.RUnlock()
	return d.name
}

// GetStatus returns the device status state.
func (d *Device) GetStatus() status {
	d.mutex.RLock()
	defer d.mutex.RUnlock()
	return d.status
}

// GetPlayback returns the device playback state.
func (d *Device) GetPlayback() playback {
	d.mutex.RLock()
	defer d.mutex.RUnlock()
	return d.playback
}

// Play begins playback of the current track.
func (d *Device) Play() (err error) {
	return d.setPlayback("play")
}

// Pause pauses playback of the current track.
func (d *Device) Pause() (err error) {
	return d.setPlayback("pause")
}

// Next plays the next track.
func (d *Device) Next() (err error) {
	return d.setPlayback("next")
}

// Next plays the previous track.
func (d *Device) Previous() (err error) {
	return d.setPlayback("previous")
}

// SetVolume sets the volume to the given value.
func (d *Device) SetVolume(volume uint8) (err error) {
	params := map[string]interface{}{"volume": volume}
	resp, err := d.requestWithParams("GET", "main/setVolume", params)
	if err == nil {
		_, err = decodeResponse(resp)
	}

	return err
}

// IncreaseVolume increases the volume by the given value.
func (d *Device) IncreaseVolume(step uint8) (err error) {
	params := map[string]interface{}{"volume": "up", "step": step}
	resp, err := d.requestWithParams("GET", "main/setVolume", params)
	if err == nil {
		_, err = decodeResponse(resp)
	}

	return err
}

// DecreaseVolume decreases the volume by the given value.
func (d *Device) DecreaseVolume(step uint8) (err error) {
	params := map[string]interface{}{"volume": "down", "step": step}
	resp, err := d.requestWithParams("GET", "main/setVolume", params)
	if err == nil {
		_, err = decodeResponse(resp)
	}

	return err
}

// SetMute mutes and unmutes the volume.
func (d *Device) SetMute(mute bool) (err error) {
	params := map[string]interface{}{"enable": mute}
	resp, err := d.requestWithParams("GET", "main/setMute", params)
	if err == nil {
		_, err = decodeResponse(resp)
	}

	return err
}

func (d *Device) fetchDeviceInfo() (err error) {
	resp, err := d.request("GET", "system/getDeviceInfo")
	if err == nil {
		data, err := decodeResponse(resp)
		if err == nil {
			d.id = data["device_id"].(string)
			d.model = data["model_name"].(string)
		}
	}

	return err
}

func (d *Device) fetchNetworkStatus() (err error) {
	resp, err := d.request("GET", "system/getNetworkStatus")
	if err == nil {
		data, err := decodeResponse(resp)
		if err == nil {
			d.name = data["network_name"].(string)
		}
	}

	return err
}

func (d *Device) fetchStatus() (err error) {
	resp, err := d.request("GET", "main/getStatus")
	if err == nil {
		defer resp.Body.Close()
		err = json.NewDecoder(resp.Body).Decode(&d.status)
	}

	return err
}

func (d *Device) fetchPlayback() (err error) {
	resp, err := d.request("GET", "netusb/getPlayInfo")
	if err == nil {
		defer resp.Body.Close()
		err = json.NewDecoder(resp.Body).Decode(&d.playback)
	}

	return err
}

func (d *Device) sync() (err error) {
	err = d.fetchDeviceInfo()
	if err == nil {
		err = d.fetchNetworkStatus()
		if err == nil {
			err = d.fetchStatus()
			if err == nil {
				err = d.fetchPlayback()
			}
		}
	}

	return err
}

func (d *Device) processEvent(e event) (err error) {
	if d.id != e["device_id"] {
		panic(fmt.Errorf("unmatched device id"))
	} else {
		delete(e, "device_id")
	}

	d.mutex.Lock()
	defer d.mutex.Unlock()

	old := *d
	if main, ok := e["main"].(map[string]interface{}); ok {
		if main["status_updated"] == true {
			err = d.fetchStatus()
			delete(main, "status_updated")
		}
		if main["signal_info_updated"] == true {
			delete(main, "signal_info_updated")
		}
		err = updateIn(&d.status, main)
		delete(e, "main")
	}

	if netusb, ok := e["netusb"].(map[string]interface{}); ok {
		if netusb["play_info_updated"] == true {
			err = d.fetchPlayback()
			delete(netusb, "play_info_updated")
		}
		if netusb["recent_updated"] == true {
			delete(netusb, "recent_updated")
		}
		if playQueue, ok := netusb["play_queue"].(map[string]interface{}); ok {
			if playQueue["updated"] == true {
				delete(playQueue, "updated")
			}
			delete(netusb, "play_queue")
		}
		err = updateIn(&d.playback, netusb)
		delete(e, "netusb")
	}

	diff := pretty.Diff(old, *d)
	if len(diff) > 0 {
		log.Println(d.id, "=>", diff)
	}

	if len(e) > 0 {
		err = fmt.Errorf("unhandled fragment in MusicCast event %v", e)
	}

	return err
}

func (d *Device) setPlayback(playback string) (err error) {
	params := map[string]interface{}{"playback": playback}
	resp, err := d.requestWithParams("GET", "netusb/setPlayback", params)
	if err == nil {
		_, err = decodeResponse(resp)
	}

	return err
}

func (d *Device) request(m string, p string) (resp *http.Response, err error) {
	return d.requestWithParams(m, p, make(map[string]interface{}))
}

func (d *Device) requestWithParams(m string, p string, q map[string]interface{}) (resp *http.Response, err error) {
	url := d.extendedControlBaseURL
	url.Path = path.Join(url.Path, p)

	req, err := http.NewRequest(m, url.String(), nil)
	if err == nil {
		req.Header.Add("X-AppName", "MusicCast/1.50")
		req.Header.Add("X-AppPort", "41100")
		if len(q) > 0 {
			params := req.URL.Query()
			for k, v := range q {
				params.Add(k, fmt.Sprint(v))
			}
			req.URL.RawQuery = params.Encode()
		}
		resp, err = d.httpClient.Do(req)
	}

	return resp, err
}

func decodeResponse(resp *http.Response) (data map[string]interface{}, err error) {
	defer resp.Body.Close()
	err = json.NewDecoder(resp.Body).Decode(&data)
	if err == nil {
		resp_code := data["response_code"].(float64)
		delete(data, "response_code")
		if resp_code != 0 {
			err = fmt.Errorf("extended control error %d", resp_code)
		}
	}
	return data, err
}

func updateIn(field interface{}, update map[string]interface{}) (err error) {
	if len(update) > 0 {
		data, err := json.Marshal(update)
		if err == nil {
			err = json.Unmarshal(data, field)
		}
	}

	return err
}

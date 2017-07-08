package main

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
)

type Status struct {
	Input     string `json:"input"`
	Power     string `json:"power"`
	Sleep     uint8  `json:"sleep"`
	Volume    uint8  `json:"volume"`
	Mute      bool   `json:"mute"`
	MaxVolume uint8  `json:"max_volume"`
}

type Playback struct {
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
	DeviceID               string `json:"device_id"`
	DeviceModel            string `json:"model_name"`
	NetworkName            string `json:"network_name"`
	Status                 Status
	Playback               Playback
	httpClient             *http.Client
	avTransport            *av1.AVTransport1
	extendedControlBaseURL url.URL
}

type Event map[string]interface{}

var availableDevices = make(map[string]*Device)

// Discover attempts to find MusicCast devices on the local network.
func Discover() (err error) {
	maybeRootDevices, err := upnp.DiscoverDevices("urn:schemas-upnp-org:device:MediaRenderer:1")
	if err == nil {
		for _, maybeRoot := range maybeRootDevices {
			d, err := NewDevice(maybeRoot)
			log.Println(d.DeviceID, "is available")
			if err == nil {
				availableDevices[d.DeviceID] = d
			}
		}
	}

	return err
}

// Listen listens and dispatches incoming MusicCast events.
func Listen() (err error) {
	listenAddr, err := net.ResolveUDPAddr("udp", ":41100")
	if err == nil {
		conn, err := net.ListenUDP("udp", listenAddr)
		if err == nil {
			defer conn.Close()
			buf := make([]byte, 1024)
			for {
				size, _, err := conn.ReadFromUDP(buf)
				if err != nil {
					log.Panic(err)
				}
				var payload Event
				err = json.Unmarshal(buf[0:size], &payload)
				if err != nil {
					log.Panic(err)
				}
				d := availableDevices[payload["device_id"].(string)]
				err = d.processEvent(payload)
				if err != nil {
					log.Panic(err)
				}
			}
		}
	}

	return err
}

// NewDevice creates a new Device from the given UPnP root device.
func NewDevice(maybeRoot upnp.MaybeRootDevice) (device *Device, err error) {
	err = maybeRoot.Err
	if err == nil {
		extendedControlURL := maybeRoot.Root.Device.PresentationURL.URL
		extendedControlURL.Path = path.Join(extendedControlURL.Path, "YamahaExtendedControl", "v1")
		avTransportClients, err := av1.NewAVTransport1ClientsFromRootDevice(maybeRoot.Root, maybeRoot.Location)
		if err == nil {
			device = &Device{"", "", "", Status{}, Playback{}, &http.Client{}, avTransportClients[0], extendedControlURL}
			err = device.sync()
		}
	}

	return device, err
}

func (d *Device) fetchDeviceInfo() (err error) {
	resp, err := d.request("GET", "system/getDeviceInfo")
	if err == nil {
		defer resp.Body.Close()
		err = json.NewDecoder(resp.Body).Decode(&d)
	}

	return err
}

func (d *Device) fetchNetworkStatus() (err error) {
	resp, err := d.request("GET", "system/getNetworkStatus")
	if err == nil {
		defer resp.Body.Close()
		err = json.NewDecoder(resp.Body).Decode(&d)
	}

	return err
}

func (d *Device) fetchStatus() (err error) {
	resp, err := d.request("GET", "main/getStatus")
	if err == nil {
		defer resp.Body.Close()
		err = json.NewDecoder(resp.Body).Decode(&d.Status)
	}

	return err
}

func (d *Device) fetchPlayback() (err error) {
	resp, err := d.request("GET", "netusb/getPlayInfo")
	if err == nil {
		defer resp.Body.Close()
		err = json.NewDecoder(resp.Body).Decode(&d.Playback)
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

func (d *Device) processEvent(e Event) (err error) {
	old := *d
	delete(e, "device_id")
	if main, ok := e["main"].(map[string]interface{}); ok {
		if main["status_updated"] == true {
			err = d.fetchStatus()
			delete(main, "status_updated")
		}
		if main["signal_info_updated"] == true {
			delete(main, "signal_info_updated")
		}
		err = updateIn(&d.Status, main)
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
		err = updateIn(&d.Playback, netusb)
		delete(e, "netusb")
	}

	log.Println(d.DeviceID, "=>", pretty.Diff(old, *d))

	if len(e) > 0 {
		err = fmt.Errorf("unhandled fragment in MusicCast event %v", e)
	}

	return err
}

func (d *Device) request(m string, p string) (resp *http.Response, err error) {
	return d.requestWithParams(m, p, make(map[string]string))
}

func (d *Device) requestWithParams(m string, p string, q map[string]string) (resp *http.Response, err error) {
	url := d.extendedControlBaseURL
	url.Path = path.Join(url.Path, p)

	req, err := http.NewRequest(m, url.String(), nil)
	if err == nil {
		req.Header.Add("X-AppName", "MusicCast/1.50")
		req.Header.Add("X-AppPort", "41100")
		if len(q) > 0 {
			params := req.URL.Query()
			for k, v := range q {
				params.Add(k, v)
			}
			req.URL.RawQuery = params.Encode()
		}
		resp, err = d.httpClient.Do(req)
	}

	return resp, err
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

func main() {
	err := Discover()
	if err != nil {
		log.Fatal(err)
	}

	go Listen()

	select {}
}

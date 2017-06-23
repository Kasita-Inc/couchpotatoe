package main

import (
	"encoding/json"
	upnp "github.com/huin/goupnp"
	"github.com/huin/goupnp/dcps/av1"
	"log"
	"net"
	"net/http"
	"net/url"
	"path"
)

type Status struct {
	Power     string `json:"power"`
	Sleep     uint8  `json:"sleep"`
	Volume    uint8  `json:"volume"`
	Mute      bool   `json:"mute"`
	MaxVolume uint8  `json:"max_volume"`
	Input     string `json:"input"`
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
	ExtendedControlBaseURL url.URL
	AVTransport            *av1.AVTransport1
	Status                 Status
	Playback               Playback
}

func NewDevice(maybeRoot upnp.MaybeRootDevice) (device *Device, err error) {
	err = maybeRoot.Err
	if err == nil {
		extendedControlURL := maybeRoot.Root.Device.PresentationURL.URL
		extendedControlURL.Path = path.Join(extendedControlURL.Path, "YamahaExtendedControl", "v1")
		avTransportClients, err := av1.NewAVTransport1ClientsFromRootDevice(maybeRoot.Root, maybeRoot.Location)
		if err == nil {
			device = &Device{extendedControlURL, avTransportClients[0], Status{}, Playback{}}
			err = device.SyncStatus()
			if err == nil {
				err = device.SyncPlayback()
			}
		}
	}

	return device, err
}

func (d *Device) SyncStatus() (err error) {
	url := d.ExtendedControlBaseURL
	url.Path = path.Join(d.ExtendedControlBaseURL.Path, "main", "getStatus")

	req, err := http.NewRequest("GET", url.String(), nil)
	if err == nil {
		resp, err := extendedControlRequest(req)
		if err == nil {
			defer resp.Body.Close()
			err = json.NewDecoder(resp.Body).Decode(&d.Status)
		}
	}

	return err
}

func (d *Device) SyncPlayback() (err error) {
	url := d.ExtendedControlBaseURL
	url.Path = path.Join(d.ExtendedControlBaseURL.Path, "netusb", "getPlayInfo")

	resp, err := http.Get(url.String())
	if err == nil {
		defer resp.Body.Close()
		err = json.NewDecoder(resp.Body).Decode(&d.Playback)
	}

	return err
}

func Discover() (devices []*Device, err error) {
	maybeRootDevices, err := upnp.DiscoverDevices("urn:schemas-upnp-org:device:MediaRenderer:1")
	if err == nil {
		for _, maybeRoot := range maybeRootDevices {
			dev, err := NewDevice(maybeRoot)
			if err == nil {
				devices = append(devices, dev)
			}
		}
	}

	return devices, nil
}

func Listen(ch chan map[string]interface{}) {
	listenAddr, err := net.ResolveUDPAddr("udp", ":41100")
	if err == nil {
		conn, err := net.ListenUDP("udp", listenAddr)
		if err == nil {
			defer conn.Close()
			buf := make([]byte, 1024)
			for {
				size, _, err := conn.ReadFromUDP(buf)
				if err == nil {
					var payload map[string]interface{}
					json.Unmarshal(buf[0:size], &payload)
					ch <- payload
				}
			}
		}
	}
}

func extendedControlRequest(req *http.Request) (resp *http.Response, err error) {
	req.Header.Add("X-AppName", "MusicCast/1.50")
	req.Header.Add("X-AppPort", "41100")
	client := &http.Client{}
	return client.Do(req)
}

func main() {
	devices, err := Discover()
	if err != nil {
		log.Fatal(err)
	}

	ch := make(chan map[string]interface{})
	go Listen(ch)

	for _, device := range devices {
		log.Println(device.Status)
		log.Println(device.Playback)
	}

	for {
		payload := <-ch
		log.Println(payload)
	}
}

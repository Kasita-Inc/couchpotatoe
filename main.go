package main

import (
	"encoding/json"
	upnp "github.com/huin/goupnp"
	"github.com/huin/goupnp/dcps/av1"
	"log"
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
}

func (d *Device) GetStatus() (status Status, err error) {
	url := d.ExtendedControlBaseURL
	url.Path = path.Join(d.ExtendedControlBaseURL.Path, "main", "getStatus")

	resp, err := http.Get(url.String())
	if err == nil {
		err = json.NewDecoder(resp.Body).Decode(&status)
	}

	return status, err
}

func (d *Device) GetPlayback() (playback Playback, err error) {
	url := d.ExtendedControlBaseURL
	url.Path = path.Join(d.ExtendedControlBaseURL.Path, "netusb", "getPlayInfo")

	resp, err := http.Get(url.String())
	if err == nil {
		err = json.NewDecoder(resp.Body).Decode(&playback)
	}

	return playback, err
}

func Discover() (devices []*Device, err error) {
	maybeRootDevices, err := upnp.DiscoverDevices("urn:schemas-upnp-org:device:MediaRenderer:1")
	if err == nil {
		for _, maybeRoot := range maybeRootDevices {
			if maybeRoot.Err == nil {
				extendedControlURL := maybeRoot.Root.Device.PresentationURL.URL
				extendedControlURL.Path = path.Join(extendedControlURL.Path, "YamahaExtendedControl", "v1")
				avTransportClients, err := av1.NewAVTransport1ClientsFromRootDevice(maybeRoot.Root, maybeRoot.Location)
				if err == nil {
					devices = append(devices, &Device{extendedControlURL, avTransportClients[0]})
				}
			}
		}
	}

	return devices, nil
}

func main() {
	devices, err := Discover()
	if err != nil {
		log.Fatal(err)
	}
}

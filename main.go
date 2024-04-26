package main

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
)

const (
	audioFileName = "output.mulaw"
)

func doSignaling(w http.ResponseWriter, r *http.Request) {
	if r.Method == "OPTIONS" {
		return
	}

	operation := string(r.Header.Get("X-Operation"))

	offer, err := io.ReadAll(r.Body)
	if err != nil {
		panic(err)
	}

	m := &webrtc.MediaEngine{}
	if err := m.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypePCMU, ClockRate: 8000, Channels: 0, SDPFmtpLine: "", RTCPFeedback: nil},
		PayloadType:        8,
	}, webrtc.RTPCodecTypeAudio); err != nil {
		panic(err)
	}

	peerConnection, err := webrtc.NewAPI(webrtc.WithMediaEngine(m)).NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		panic(err)
	}

	audioTrack, err := webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypePCMU}, "audio", "pion")
	if err != nil {
		panic(err)
	}

	_, err = peerConnection.AddTrack(audioTrack)
	if err != nil {
		panic(err)
	}

	if operation == "send" {
		peerConnection.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) { //nolint: revive
			file, err := os.OpenFile(audioFileName, os.O_WRONLY|os.O_CREATE, 0600)
			if err != nil {
				panic(err)
			}

			for {
				rtpPkt, _, err := track.ReadRTP()
				if errors.Is(io.EOF, err) {
					return
				} else if err != nil {
					panic(err)
				}

				if _, err = file.Write(rtpPkt.Payload); err != nil {
					panic(err)
				}
			}
		})

	} else if operation == "receive" {
		go func() {
			readBuff := make([]byte, 1024)
			file, err := os.Open(audioFileName)
			if err != nil {
				panic(err)
			}

			for {
				_, err := file.Read(readBuff)
				if errors.Is(io.EOF, err) {
					return
				} else if err != nil {
					panic(err)
				}

				if err = audioTrack.WriteSample(media.Sample{Data: readBuff, Duration: time.Millisecond * 128}); err != nil {
					panic(err)
				}

				time.Sleep(time.Millisecond * 128)
			}
		}()
	}

	// Set the handler for ICE connection state
	// This will notify you when the peer has connected/disconnected
	peerConnection.OnICEConnectionStateChange(func(connectionState webrtc.ICEConnectionState) {
		fmt.Printf("ICE Connection State has changed: %s\n", connectionState.String())

		if connectionState == webrtc.ICEConnectionStateFailed {
			peerConnection.Close()
		}
	})

	if err = peerConnection.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: string(offer)}); err != nil {
		panic(err)
	}

	// Create channel that is blocked until ICE Gathering is complete
	gatherComplete := webrtc.GatheringCompletePromise(peerConnection)

	answer, err := peerConnection.CreateAnswer(nil)
	if err != nil {
		panic(err)
	} else if err = peerConnection.SetLocalDescription(answer); err != nil {
		panic(err)
	}

	<-gatherComplete

	w.Header().Add("Location", "/doSignaling")
	w.WriteHeader(http.StatusCreated)
	fmt.Fprint(w, peerConnection.LocalDescription().SDP)
}

func main() {
	http.Handle("/", http.FileServer(http.Dir(".")))
	http.HandleFunc("/doSignaling", doSignaling)

	fmt.Println("Open http://localhost:8080 to access this demo")
	// nolint: gosec
	panic(http.ListenAndServe(":8080", nil))
}

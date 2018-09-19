package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/gorilla/websocket"
)
import "net/http"

type Message struct {
	Type   string                 `json:"type"`
	Params map[string]interface{} `json:"params"`
}

type AuthParams struct {
	Version  int     `json:"version"`
	Site_key string  `json:"site_key"`
	Type     string  `json:"type"`
	User     *string `json:"user"`
	Goal     int     `json:"goal"`
}

type Auth struct {
	Type   string     `json:"type"`
	Params AuthParams `json:"params"`
}

func get_a_job(results chan Message, URL string) {
	header := make(http.Header)
	header.Add("Sec-WebSocket-Protocol", "event-stream-protocol")
	header.Add("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_13_2) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/63.0.3239.84 Safari/537.36")

	sock, _, err := websocket.DefaultDialer.Dial(URL, header)
	if err != nil {
		return
	}
	defer sock.Close()
	c := Auth{Type: "auth", Params: AuthParams{Version: 7, Site_key: "eFaJ1ABexe4wEgu7T2ntZ4nEwWh487Fd", Type: "token", Goal: 256, User: nil}}
	sock.WriteJSON(&c)

	for {

		m := Message{}
		//fmt.Fprintf(os.Stderr, "Waiting for new message from server\n")
		err := sock.ReadJSON(&m)
		if err != nil {
			return
		}
		if m.Type == "authed" {
			continue
		}
		//fmt.Fprintf(os.Stderr, "Got message %s\n", m)
		if m.Type == "job" {
			results <- m
			return
		}
		if m.Type == "banned" {
			fmt.Fprintf(os.Stderr, "Got banned...\n")
			return
		}
		return
	}
}

var url = flag.String("url", "wss://ws001.coinhive.com/proxy", "url")

func main() {
	flag.Parse()

	results := make(chan Message, 20)

	enc := json.NewEncoder(os.Stdout)
	os.Stdout.Sync()
	enc.SetEscapeHTML(false)

	for {

		select {
		case <-time.After(time.Millisecond * 500):
			go get_a_job(results, *url)
			for len(results) > 0 {
				m := <-results
				enc.Encode(m)
			}

		}
	}
}

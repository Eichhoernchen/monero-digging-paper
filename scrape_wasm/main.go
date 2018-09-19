package main

import (
	"bufio"
	"encoding/json"
	"flag"
	//"fmt"
	"log"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/wirepair/gcd/gcdmessage"

	"github.com/wirepair/gcd"
	"github.com/wirepair/gcd/gcdapi"
)

var path string
var dir string
var port string
var num_concurrent int
var chrome_sync sync.Mutex
var chrome_crashed chan *gcd.Gcd

func init() {
	switch runtime.GOOS {
	case "windows":
		flag.StringVar(&path, "chrome", "C:\\Program Files (x86)\\Google\\Chrome\\Application\\chrome.exe", "path to chrome")
		flag.StringVar(&dir, "dir", "C:\\temp\\", "user directory")
	case "darwin":
		flag.StringVar(&path, "chrome", "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome", "path to chrome")
		flag.StringVar(&dir, "dir", "/tmp/", "user directory")
	case "linux":
		flag.StringVar(&path, "chrome", "/usr/bin/chromium-browser", "path to chrome")
		flag.StringVar(&dir, "dir", "/tmp/", "user directory")
	}

	flag.StringVar(&port, "port", "9222", "Debugger port")

	flag.IntVar(&num_concurrent, "parallel", 100, "Number of parallel tabs")
}

func serialize_request(input interface{}) (string, error) {
	out, err := json.Marshal(input)
	if err != nil {
		return "", nil
	}
	return string(out), nil
}

type WasmScript struct {
	request_id int64
	Url        string
	Source     string
}

type WebsocketFrame struct {
	Timestamp float64
	Direction string
	Frame     *gcdapi.NetworkWebSocketFrame
}

type Websocket struct {
	request_id string
	Url        string
	Frames     []WebsocketFrame
}

type PageResult struct {
	Destination string        `json:"destination"`
	Wasmscripts []*WasmScript `json:"wasm_scripts,omitempty"`
	Documenturl *string       `json:"document_url,omitempty"`
	Html        *string       `json:"html,omitempty"`
	Websockets  []*Websocket  `json:"websockets,omitempty"`
	Statuscode  int           `json:"status_code"`
	Error       *string       `json:"error,omitempty"`
}

type InternalPageData struct {
	main_request_id        string
	status_code            int
	destination            string
	pending_locker         sync.RWMutex
	pending_script_results map[int64]*WasmScript
	scripts                []*WasmScript
	dom                    string
	doc_url                string
	websocket              map[string]*Websocket
	error                  string
}

func scrape_wasm_and_dom(target *gcd.ChromeTarget, destination string, result_chan chan *PageResult, term_func func()) {
	defer term_func()
	page_data := InternalPageData{pending_locker: sync.RWMutex{}, pending_script_results: make(map[int64]*WasmScript), destination: destination, scripts: nil, websocket: make(map[string]*Websocket)}
	on_page_load_fired := make(chan interface{}, 5)

	last_dom_event := time.NewTimer(2 * time.Second)
	last_dom_event.Stop()

	domChangedEvent := func(targ *gcd.ChromeTarget, payload []byte) {
		/*
			var msg map[string]interface{}
			json.Unmarshal(payload, &msg)
			log.Printf("DOM Event Fired %s", msg["method"].(string))
		*/
		last_dom_event.Reset(2 * time.Second)
	}

	_, err := target.Security.SetIgnoreCertificateErrors(true)
	if err != nil {
		log.Printf("Failed settings to ignore cert errors: %s", err.Error())
	}

	//subscribe to page load
	target.Subscribe("Page.loadEventFired", func(targ *gcd.ChromeTarget, v []byte) {
		on_page_load_fired <- 1
	})
	/*
		target.Subscribe("Page.lifecycleEvent", func(targ *gcd.ChromeTarget, payload []byte) {
			lifecycleevent := &gcdapi.PageLifecycleEventEvent{}
			json.Unmarshal(payload, lifecycleevent)
			log.Printf("Lifecylce event %s fired", lifecycleevent.Params.Name)
		})
	*/
	//lock := sync.Mutex{}
	//subscribe to page load
	target.Subscribe("Target.attachedToTarget", func(targ *gcd.ChromeTarget, payload []byte) {
		//lock.Lock()
		//defer lock.Unlock()
		attachedEvent := &gcdapi.TargetAttachedToTargetEvent{}
		err := json.Unmarshal(payload, attachedEvent)
		if err != nil {
			log.Fatalf("error decoding attachedToTarget")
		}

		//log.Printf("Subscribe to runtime")
		msg, err := serialize_request(&gcdmessage.ParamRequest{Id: targ.GetId(), Method: "Runtime.enable"})
		if err != nil {
			log.Fatalf("error serializing Runtime.enable")
		}
		_, err = targ.TargetApi.SendMessageToTarget(msg, attachedEvent.Params.SessionId, attachedEvent.Params.TargetInfo.TargetId)
		if err != nil {
			log.Printf("error sending Runtime.enable: %s", err.Error())
			return
		}
		//log.Printf("Response: %s", resp)

		//log.Printf("Subscribe to debugger")
		msg, err = serialize_request(&gcdmessage.ParamRequest{Id: targ.GetId(), Method: "Debugger.enable"})
		if err != nil {
			log.Fatalf("error serializing Debugger.enable")
		}
		_, err = targ.TargetApi.SendMessageToTarget(msg, attachedEvent.Params.SessionId, attachedEvent.Params.TargetInfo.TargetId)
		if err != nil {
			log.Printf("error sending Debugger.enable: %s", err.Error())
			return
		}

		msg, err = serialize_request(&gcdmessage.ParamRequest{Id: targ.GetId(), Method: "Debugger.setSkipAllPauses", Params: &gcdapi.DebuggerSetSkipAllPausesParams{Skip: true}})
		if err != nil {
			log.Fatalf("error serializing Debugger.setSkipAllPauses")
		}
		_, err = targ.TargetApi.SendMessageToTarget(msg, attachedEvent.Params.SessionId, attachedEvent.Params.TargetInfo.TargetId)
		if err != nil {
			log.Printf("error sending Debugger.setSkipAllPauses: %s", err.Error())
			return
		}

		//log.Printf("Response: %s", resp)

		//log.Printf("Running target")
		msg, err = serialize_request(&gcdmessage.ParamRequest{Id: targ.GetId(), Method: "Runtime.runIfWaitingForDebugger"})
		if err != nil {
			log.Fatalf("error serializing Runtime.runIfWaitingForDebugger")
		}
		_, err = targ.TargetApi.SendMessageToTarget(msg, attachedEvent.Params.SessionId, attachedEvent.Params.TargetInfo.TargetId)
		if err != nil {
			log.Printf("error sending Runtime.runIfWaitingForDebugger: %s", err.Error())
			return
		}

		//log.Printf("Response: %s", resp)

	})

	target.Subscribe("Target.receivedMessageFromTarget", func(targ *gcd.ChromeTarget, payload []byte) {

		targetmessage := &gcdapi.TargetReceivedMessageFromTargetEvent{}
		err := json.Unmarshal(payload, targetmessage)
		if err != nil {
			log.Printf("error decoding receivedMessageFromTarget")
			return
		}
		//log.Printf("Target messag received: %s", string(payload))

		var deparse map[string]interface{}

		err = json.Unmarshal([]byte(targetmessage.Params.Message), &deparse)
		if err != nil {
			return
		}
		if val, ok := deparse["method"]; ok {
			//log.Printf("Got Message with method: %s", val)
			switch val {
			case "Debugger.scriptParsed":

				script_parsed := &gcdapi.DebuggerScriptParsedEvent{}
				json.Unmarshal([]byte(targetmessage.Params.Message), script_parsed)

				if !strings.HasPrefix(script_parsed.Params.Url, "wasm") {
					return
				}

				response_id := targ.GetId()
				msg, err := serialize_request(&gcdmessage.ParamRequest{Id: response_id, Method: "Debugger.getScriptSource", Params: &gcdapi.DebuggerGetScriptSourceParams{ScriptId: script_parsed.Params.ScriptId}})
				if err != nil {
					log.Printf("error serializing Debugger.getScriptSourcer")
					return
				}
				page_data.pending_locker.Lock()
				_, err = targ.TargetApi.SendMessageToTarget(msg, targetmessage.Params.SessionId, targetmessage.Params.TargetId)
				if err != nil {
					log.Printf("Error getting script source: %s", err.Error())
					page_data.pending_locker.Unlock()
					return
				}
				page_data.pending_script_results[response_id] = &WasmScript{request_id: response_id, Url: script_parsed.Params.Url}
				page_data.pending_locker.Unlock()

				//log.Printf("Result from getScriptSource: %s", resp)
			}
		}
		if val, ok := deparse["result"].(map[string]interface{}); ok {
			result_id := int64(deparse["id"].(float64))
			//log.Printf("Got Message with result_id: %s", result_id)
			if scriptSource, ok := val["scriptSource"].(string); ok {
				page_data.pending_locker.RLock()
				page_data.pending_script_results[result_id].Source = scriptSource
				page_data.pending_locker.RUnlock()
			}
		}

	})

	//target.Emulation.SetVirtualTimePolicyWithParams(&gcdapi.EmulationSetVirtualTimePolicyParams{Policy: "pauseIfNetworkFetchesPending", Budget: 5000, WaitForNavigation: true})

	target.Debugger.Enable()
	target.Debugger.SetSkipAllPauses(true)
	/*
		target.Subscribe("Debugger.paused", func(targ *gcd.ChromeTarget, payload []byte) {
			log.Printf("Paused")
		})
	*/
	target.Subscribe("Debugger.scriptParsed", func(targ *gcd.ChromeTarget, payload []byte) {

		script_parsed := &gcdapi.DebuggerScriptParsedEvent{}
		err := json.Unmarshal(payload, script_parsed)
		if err != nil {
			log.Printf("error decoding Debugger.scriptParsed")
			return
		}
		if !strings.HasPrefix(script_parsed.Params.Url, "wasm") {
			return
		}

		// okay request the script source

		script_source, err := targ.Debugger.GetScriptSource(script_parsed.Params.ScriptId)
		if err != nil {
			log.Printf("error decoding Debugger.GetScriptSource")
			return
		}
		page_data.pending_locker.Lock()
		page_data.scripts = append(page_data.scripts, &WasmScript{Source: script_source, Url: script_parsed.Params.Url})
		page_data.pending_locker.Unlock()
	})

	target.Network.EnableWithParams(&gcdapi.NetworkEnableParams{})

	target.Subscribe("Network.requestWillBeSent", func(targ *gcd.ChromeTarget, payload []byte) {
		request_event := &gcdapi.NetworkRequestWillBeSentEvent{}
		err := json.Unmarshal(payload, request_event)
		if err != nil {
			log.Printf("error decoding Network.requestWillBeSent")
			return
		}
		// we only want to know if the initial request succeeded
		page_data.main_request_id = request_event.Params.RequestId
		targ.Unsubscribe("Network.requestWillBeSent")
		//log.Printf("Request will be sent: %s", string(payload))
	})

	target.Subscribe("Network.responseReceived", func(targ *gcd.ChromeTarget, payload []byte) {
		response_event := &gcdapi.NetworkResponseReceivedEvent{}
		err := json.Unmarshal(payload, response_event)
		if err != nil {
			log.Printf("error decoding Network.responseReceived")
			return
		}
		if page_data.main_request_id == response_event.Params.RequestId {
			targ.Unsubscribe("Network.responseReceived")
			page_data.status_code = response_event.Params.Response.Status
			if page_data.status_code >= 400 {
				on_page_load_fired <- 1
			}
		}
		//log.Printf("Response received: %s", string(payload))
	})

	target.Subscribe("Network.loadingFailed", func(targ *gcd.ChromeTarget, payload []byte) {
		loadingfailed_event := &gcdapi.NetworkLoadingFailedEvent{}
		err := json.Unmarshal(payload, loadingfailed_event)
		if err != nil {
			log.Printf("error decoding Network.loadingFailed")
			return
		}
		if loadingfailed_event.Params.RequestId == page_data.main_request_id {
			targ.Unsubscribe("Network.loadingFailed")
			page_data.error = loadingfailed_event.Params.ErrorText
			page_data.status_code = 404
			on_page_load_fired <- 1
		}

	})

	target.Subscribe("Network.webSocketCreated", func(targ *gcd.ChromeTarget, payload []byte) {

		websocket_created := &gcdapi.NetworkWebSocketCreatedEvent{}
		err := json.Unmarshal(payload, websocket_created)
		if err != nil {
			log.Printf("error decoding Network.webSocketCreated")
			return
		}
		page_data.pending_locker.Lock()
		page_data.websocket[websocket_created.Params.RequestId] = &Websocket{request_id: websocket_created.Params.RequestId, Url: websocket_created.Params.Url, Frames: nil}
		page_data.pending_locker.Unlock()

	})

	target.Subscribe("Network.webSocketFrameReceived", func(targ *gcd.ChromeTarget, payload []byte) {

		websocket_framercv := &gcdapi.NetworkWebSocketFrameReceivedEvent{}
		err := json.Unmarshal(payload, websocket_framercv)
		if err != nil {
			log.Printf("error decoding Network.webSocketCreated")
			return
		}
		page_data.pending_locker.Lock()
		page_data.websocket[websocket_framercv.Params.RequestId].Frames = append(page_data.websocket[websocket_framercv.Params.RequestId].Frames, WebsocketFrame{Timestamp: websocket_framercv.Params.Timestamp, Direction: "Received", Frame: websocket_framercv.Params.Response})
		page_data.pending_locker.Unlock()

	})

	target.Subscribe("Network.webSocketFrameSent", func(targ *gcd.ChromeTarget, payload []byte) {

		websocket_framesnd := &gcdapi.NetworkWebSocketFrameSentEvent{}
		err := json.Unmarshal(payload, websocket_framesnd)
		if err != nil {
			log.Printf("error decoding Network.webSocketCreated")
			return
		}
		page_data.pending_locker.Lock()
		page_data.websocket[websocket_framesnd.Params.RequestId].Frames = append(page_data.websocket[websocket_framesnd.Params.RequestId].Frames, WebsocketFrame{Timestamp: websocket_framesnd.Params.Timestamp, Direction: "Sent", Frame: websocket_framesnd.Params.Response})
		page_data.pending_locker.Unlock()

	})

	target.DOM.Enable()

	target.Subscribe("DOM.attributeModified", domChangedEvent)
	target.Subscribe("DOM.attributeRemoved", domChangedEvent)
	//target.Subscribe("DOM.characterDataModified", domChangedEvent)
	//target.Subscribe("DOM.childNodeCountUpdated", domChangedEvent)
	target.Subscribe("DOM.childNodeInserted", domChangedEvent)
	target.Subscribe("DOM.childNodeRemoved", domChangedEvent)
	//target.Subscribe("DOM.distributedNodesUpdated", domChangedEvent)
	//target.Subscribe("DOM.inlineStyleInvalidated", domChangedEvent)
	target.Subscribe("DOM.pseudoElementAdded", domChangedEvent)
	target.Subscribe("DOM.pseudoElementRemoved", domChangedEvent)
	target.Subscribe("DOM.setChildNodes", domChangedEvent)
	//target.Subscribe("DOM.shadowRootPopped", domChangedEvent)
	//target.Subscribe("DOM.shadowRootPushed", domChangedEvent)

	target.TargetApi.SetAutoAttach(true, true)
	// get the Page API and enable it
	if _, err := target.Page.Enable(); err != nil {
		log.Printf("error getting page: %s\n", err)
		page_data.error = err.Error()
		page_data.status_code = 404
		goto exit
	}

	go func() {
		_, _, _, err = target.Page.NavigateWithParams(&gcdapi.PageNavigateParams{Url: destination}) // navigate
		if err != nil {
			if err.Error() == "tab is shutting down" {
				page_data.error = "net::ERR_CONNECTION_TIMED_OUT"
			} else {
				log.Printf("Error navigating: %s\n", err)
				page_data.error = err.Error()
			}
			page_data.status_code = 404
			on_page_load_fired <- 1
		}
	}()

	// wait at most n seconds or some fancy event
	select {
	case <-time.After(15 * time.Second):
		if page_data.status_code == 0 {
			page_data.status_code = 404
			page_data.error = "net::ERR_CONNECTION_TIMED_OUT"
		}
		break
	case <-on_page_load_fired:
		if page_data.status_code < 400 {
			select {
			case <-last_dom_event.C:
				break
			case <-time.After(5 * time.Second):
				break
			}
			// get the dom
			doc, err := target.DOM.GetDocument(-1, true)
			if err == nil {
				//log.Printf("DOC: %s", doc)
				page_data.doc_url = doc.DocumentURL
				page_data.dom, err = target.DOM.GetOuterHTMLWithParams(&gcdapi.DOMGetOuterHTMLParams{NodeId: doc.NodeId})
			}
		}

		break
	}
exit:
	result := PageResult{}
	result.Destination = page_data.destination

	result.Statuscode = page_data.status_code
	if result.Statuscode < 400 {
		result.Documenturl = &page_data.doc_url
		if len(page_data.dom) > 65*1024 {
			first_65k := page_data.dom[:65*1024]
			result.Html = &first_65k
		} else {
			result.Html = &page_data.dom
		}
	} else {
		if page_data.error != "" {
			result.Error = &page_data.error
		}
	}
	result.Wasmscripts = page_data.scripts
	for _, v := range page_data.pending_script_results {
		result.Wasmscripts = append(result.Wasmscripts, v)
	}
	for _, v := range page_data.websocket {
		result.Websockets = append(result.Websockets, v)
	}
	result_chan <- &result

}

func line_from_stdin(stdin_chan chan<- string, running_chan chan<- bool) {
	fscanner := bufio.NewScanner(os.Stdin)
	maxsize := 64 * 1024 * 1024
	inbuff := make([]byte, maxsize, maxsize)
	fscanner.Buffer(inbuff, maxsize)
	for fscanner.Scan() {
		running_chan <- true
		stdin_chan <- fscanner.Text()
	}
	close(stdin_chan)
}

func main() {

	flag.Parse()
	chrome_sync = sync.Mutex{}
	chrome_crashed = make(chan *gcd.Gcd, num_concurrent)
	chrome_sync.Lock()
	debugger := gcd.NewChromeDebugger()

	// start process, specify a tmp profile path so we get a fresh profiled browser
	// set port 9222 as the debug port
	// "--blink-settings='imagesEnabled=false'",
	debugger.AddFlags([]string{"--headless", "--ignore-certificate-errors", "--allow-running-insecure-content", "--process-per-tab", "--disable-translate", "--disable-infobars", "--disable-notifications", "--disable-save-password-bubble", "--disable-background-timer-throttling", "--disable-browser-side-navigation", "--disable-domain-reliability", "--disable-add-to-shelf", "--disable-client-side-phishing-detection"})
	err := debugger.StartProcess(path, dir, port)
	if err != nil {
		log.Fatalf("Failed starting chrome: %s", err.Error())
	}
	chrome_sync.Unlock()

	enc := json.NewEncoder(os.Stdout)
	os.Stdout.Sync()
	enc.SetEscapeHTML(false)

	result_chan := make(chan *PageResult)
	running_chan := make(chan bool, num_concurrent)

	stdin_chan := make(chan string, 1)
	go line_from_stdin(stdin_chan, running_chan)

	running := true

	start := time.Now()
	num_processed := 0
	print_chan := make(chan time.Time, 1)
	print_chan <- <-time.After(0 * time.Second)

	tab_pool := make(chan *gcd.ChromeTarget, num_concurrent*2/3+num_concurrent)
	for i := cap(tab_pool); i > 0; i-- {
		tab, err := debugger.NewTab()
		if err != nil {
			log.Fatalf("Failed initialing tab pool")
		}
		tab_pool <- tab
		<-time.After(1 * time.Second)
	}

	for len(running_chan) > 0 || running {
		select {
		case chrashed := <-chrome_crashed:
			if chrashed != debugger {
				continue
			}
			chrome_sync.Lock()
			for i := len(tab_pool); i > 0; i-- {
				<-tab_pool
			}
			debugger.ExitProcess()
			debugger = gcd.NewChromeDebugger()
			debugger.AddFlags([]string{"--headless"})
			err := debugger.StartProcess(path, dir, port)
			if err != nil {
				log.Fatalf("Failed starting chrome: %s", err.Error())
			}
			// consume others stuff on this channel
			for i := 0; i < len(chrome_crashed); i++ {
				<-chrome_crashed
			}
			for i := cap(tab_pool); i > 0; i-- {
				tab, err := debugger.NewTab()
				if err != nil {
					log.Fatalf("Failed initialing tab pool")
				}
				tab_pool <- tab
				<-time.After(1 * time.Second)
			}
			chrome_sync.Unlock()
		case line, ok := <-stdin_chan:
			if ok {
				tab := <-tab_pool
				go scrape_wasm_and_dom(tab, line, result_chan, func() {
					err := debugger.CloseTab(tab)
					if err != nil {
						log.Printf("Failed closing tab: %s", err.Error())
					}

					newtab, err := debugger.NewTab()

					if err != nil {
						log.Printf("Error creating new tab: %s", err.Error())
						// if this happens we need to recreate a new chrome
						chrome_crashed <- debugger
					} else {
						tab_pool <- newtab
					}

				})
			} else {
				running = false
			}

		case <-print_chan:
			log.Printf("Current Rate: %f pages/second", float64(num_processed)/(float64(time.Since(start))/float64(time.Second)))
			go func() {
				print_chan <- <-time.After(10 * time.Second)
			}()

		case result := <-result_chan:
			enc.Encode(result)
			num_processed += 1
			<-running_chan
		}
	}
	debugger.ExitProcess() // exit when done
}

package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"coinhive-link-follower/hashing"
	"encoding/hex"

	"net/http"
)

import "github.com/gorilla/websocket"

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

type Authed struct {
	Type   string `json:"type"`
	Params struct {
		Token  string `json:"token"`
		Hashes int    `json:"hashes"`
	} `json:"params"`
}
type JobParams struct {
	Job_id string `json:"job_id"`
	Blob   string `json:"blob"`
	Target string `json:"target"`
}

type Job struct {
	Type   string    `json:"type"`
	Params JobParams `json:"params"`
}

type VerifyJobParams struct {
	Verify_id int    `json:"verify_id"`
	Nonce     string `json:"nonce"`
	Result    string `json:"result"`
	Blob      string `json:"blob"`
}

type VerifyJob struct {
	Type   string          `json:"type"`
	Params VerifyJobParams `json:"params"`
}

type VerifiedJob struct {
	Type   string            `json:"type"`
	Params VerifiedJobParams `json:"params"`
}
type VerifiedJobParams struct {
	Verify_id int  `json:"verify_id"`
	Verified  bool `json:"verified"`
}

type SubmitJobParams struct {
	//	HashesPerSecond float64 `json:"hashesPerSecond"`
	//	Hashes          int     `json:"hashes"`
	Version int    `json:"version"`
	Job_id  string `json:"job_id"`
	Nonce   string `json:"nonce"`
	Result  string `json:"result"`
}

type SubmitJob struct {
	Type   string          `json:"type"`
	Params SubmitJobParams `json:"params"`
}

func (c *Connection) Verify(job VerifyJobParams) {
	blob, _ := hex.DecodeString(job.Blob)

	input := make([]byte, 84)
	copy(input, blob)
	for i, c := 0, 0; c < len(job.Nonce); c, i = c+2, i+1 {
		b, _ := hex.DecodeString(job.Nonce[c : c+2])
		input[39+i] = b[0]
	}
	res := hashing.Hash(input, len(blob), false)
	c.WriteLock.Lock()
	c.Conn.WriteJSON(VerifiedJob{Type: "verified", Params: VerifiedJobParams{Verify_id: job.Verify_id, Verified: hex.EncodeToString(res) == job.Result}})
	c.WriteLock.Unlock()
}

func Hash(job Job) ([]byte, []byte) {
	blob, err := hex.DecodeString(job.Params.Blob)
	if err != nil {
		return nil, nil
	}
	// fix the blob sneaky coinhive fu...
	x := uint32(0)
	//blob[8:8+4] ^ 3768469278
	b := bytes.NewReader(blob[8 : 8+4])
	buf := new(bytes.Buffer)
	binary.Read(b, binary.LittleEndian, &x)
	x = x ^ 3768469278

	binary.Write(buf, binary.LittleEndian, x)

	copy(blob[8:], buf.Bytes())

	//fmt.Printf("FIXED: %s\n", hex.EncodeToString(blob))
	input := make([]byte, 84)
	copy(input, blob)
	nonce := rand.Uint32()

	input[39] = byte((nonce & 4278190080) >> 24)
	input[40] = byte((nonce & 16711680) >> 16)
	input[41] = byte((nonce & 65280) >> 8)
	input[42] = byte((nonce & 255) >> 0)

	return []byte{input[39], input[40], input[41], input[42]}, hashing.Hash(input, len(blob), false)
}

func MeetsTarget(hash []byte, target []byte) bool {
	for i := 0; i < len(target); i++ {
		hi := len(hash) - i - 1
		ti := len(target) - i - 1
		if hash[hi] > target[ti] {
			return false
		} else if hash[hi] < target[ti] {
			return true
		}
	}
	return false
}

func (c *Connection) Work() {
	c.ActualHashes = 0

	counter := 0
	for {
		c.job_lock.RLock()
		nonce, res_hash := Hash(c.current_job)
		c.ActualHashes = c.ActualHashes + 1
		counter += 1
		if MeetsTarget(res_hash, c.current_target) {
			atomic.AddUint64(c.hash_counter, uint64(counter))
			counter = 0
			//hps := float64(c.ActualHashes) / float64(elapsed_time)
			//fmt.Printf("%d hashes \n", c.ActualHashes)
			c.WriteLock.Lock()
			//j, _ := json.Marshal(SubmitJob{Type: "submit", Params: SubmitJobParams{Version: 7, Job_id: c.current_job.Params.Job_id, Nonce: hex.EncodeToString(nonce), Result: hex.EncodeToString(res_hash)}})
			//fmt.Fprintf(os.Stderr, "Committing: %s", string(j))
			err := c.Conn.WriteJSON(SubmitJob{Type: "submit", Params: SubmitJobParams{Version: 7, Job_id: c.current_job.Params.Job_id, Nonce: hex.EncodeToString(nonce), Result: hex.EncodeToString(res_hash)}})
			c.WriteLock.Unlock()
			if err != nil {
				c.job_lock.RUnlock()
				fmt.Fprintf(os.Stderr, "Failure writing json to connection %s\n", err.Error())
				return
			}
		}
		c.job_lock.RUnlock()
		if c.Goal > c.Hashes {
			continue
		} else {
			return
		}
	}
}

type Connection struct {
	Conn           *websocket.Conn
	WriteLock      sync.Mutex
	Token          string
	Hashes         int
	ActualHashes   int
	Goal           int
	Success        bool
	hash_counter   *uint64
	job_lock       sync.RWMutex
	current_job    Job
	worker_running bool
	current_target []byte
}

type Message struct {
	Type   string                 `json:"type"`
	Params map[string]interface{} `json:"params"`
}

func (c *Connection) Process() {
	for {

		m := Message{}
		//fmt.Fprintf(os.Stderr, "Waiting for new message from server\n")
		err := c.Conn.ReadJSON(&m)
		if err != nil {
			return
		}
		//fmt.Fprintf(os.Stderr, "Got message %s\n", m)
		if m.Type == "banned" {
			fmt.Fprintf(os.Stderr, "Got banned...\n")
			c.Conn.Close()
			return
		}
		if m.Type == "error" {
			fmt.Fprintf(os.Stderr, "Error :%s", m)
			c.Conn.Close()
			return
		}
		if m.Type == "authed" {
			c.Token = m.Params["token"].(string)
			c.Hashes = int(m.Params["hashes"].(float64))
			fmt.Fprintf(os.Stderr, "Got authed with token: %s already_done_hashes: %d\n", c.Token, c.Hashes)
			continue
		}
		if m.Type == "hash_accepted" {
			c.Hashes = int(m.Params["hashes"].(float64))
			fmt.Fprintf(os.Stderr, "Got accepted %d / %d\n", c.Hashes, c.Goal)
			if c.Hashes >= c.Goal {
				c.Success = true
				c.Conn.Close()
				return
			}
			c.Success = false
		}
		if m.Type == "job" {
			job := Job{Type: "job", Params: JobParams{Job_id: m.Params["job_id"].(string), Blob: m.Params["blob"].(string), Target: m.Params["target"].(string)}}
			thistarget := make([]byte, 8)
			target, _ := hex.DecodeString(job.Params.Target)
			if len(target) <= 8 {
				for i := 0; i < len(target); i++ {
					// copy rcv target to end of our target
					thistarget[len(thistarget)-i-1] = target[len(target)-i-1]
				}
				// fill rest with 255
				for i := 0; i < len(thistarget)-len(target); i++ {
					thistarget[i] = 255
				}
			} else {
				thistarget = target
			}
			c.job_lock.Lock()
			c.current_job = job
			c.current_target = thistarget
			c.job_lock.Unlock()
			if !c.worker_running {
				c.worker_running = true
				go c.Work()
			}
			continue
		}
		if m.Type == "verify" {
			job := VerifyJobParams{Verify_id: int(m.Params["verify_id"].(float64)), Nonce: m.Params["nonce"].(string), Result: m.Params["result"].(string), Blob: m.Params["blob"].(string)}
			c.Verify(job)
			continue
		}

	}
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

func gen_token_for_item(target_url string, postfix string, token string, target int, result_chan chan<- map[string]interface{}, hash_counter *uint64) {
	result := make(map[string]interface{})
	result["url"] = target_url
	result["postfix"] = postfix
	result["token"] = token
	result["target"] = target

	header := make(http.Header)
	header.Add("Sec-WebSocket-Protocol", "event-stream-protocol")
	header.Add("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_13_2) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/63.0.3239.84 Safari/537.36")

	sock, _, err := websocket.DefaultDialer.Dial("wss://ws001.coinhive.com/proxy", header)
	if err != nil {
		result["error"] = "wss"
		result_chan <- result
		return
	}
	conn := &Connection{Conn: sock, Token: "ladida", Hashes: 0, Goal: target, Success: false, hash_counter: hash_counter, WriteLock: sync.Mutex{}, job_lock: sync.RWMutex{}, worker_running: false}

	c := Auth{Type: "auth", Params: AuthParams{Version: 7, Site_key: token, Type: "token", Goal: target, User: nil}}
	conn.Conn.WriteJSON(&c)
	conn.Process()
	conn.Conn.Close()

	result["success"] = conn.Success
	result["success_token"] = conn.Token

	if conn.Success {
		resp, err := http.PostForm(target_url, url.Values{"token": {conn.Token}})
		if err != nil {
			result["error"] = "HTTP Token Submit"
		} else {
			body, err := ioutil.ReadAll(resp.Body)
			if err != nil {
				result["error"] = "Body Read"
			} else {
				var inline map[string]interface{}
				//log.Printf("%s\n", body)
				if err := json.Unmarshal([]byte(body), &inline); err != nil {
					//log.Printf("Error parsing json: %s (%s)\n", err.Error(), line)
					result["error"] = "Json decode"
				} else {
					result["final_url"] = inline["url"].(string)
				}
			}
		}
	}
	result_chan <- result
}

var num_concurrent = flag.Int("parallel", 10, "Number of parallel link follows")

func main() {
	j := VerifyJobParams{Verify_id: 3, Blob: "0707ecabd6d6058b14667834a6824b04fbf7178f9f10c5354b93eaaec0eba412aad47ef36466df00000000df77c3c4f5ade39856cea27e56a18703d3541283eb9913e16a34484a55c243f805", Result: "ef37132189a49d369f5fb69fe2feaacf7f403af94a823106c0af161b311e9900", Nonce: "735f47bd"}
	blob, _ := hex.DecodeString(j.Blob)

	input := make([]byte, 84)
	copy(input, blob)
	for i, c := 0, 0; c < len(j.Nonce); c, i = c+2, i+1 {
		b, _ := hex.DecodeString(j.Nonce[c : c+2])
		input[39+i] = b[0]
	}
	res := hashing.Hash(input, len(blob), false)
	fmt.Fprintf(os.Stderr, "Verified: %s == %s\n", hex.EncodeToString(res), j.Result)
	x, _ := hex.DecodeString("00000000000000000000000000000000000000000000000000000000000000000000000000000000000000")
	fmt.Fprintf(os.Stderr, "%X", hashing.Hash([]byte(x), len(x), false))
	orig := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	ctx := hashing.Hash_AES_ALLOC()
	if bytes.Equal(hashing.Hash_AES([]byte(orig), len(orig), true, ctx), hashing.Hash([]byte(orig), len(orig), false)) == true {
		fmt.Fprintf(os.Stderr, "aes_prefetch works\n\n")
	} else {
		fmt.Fprintf(os.Stderr, "aes_prefetch DIFFERENT\n\n")
	}
	hashing.Hash_AES_FREE(ctx)
	ctx = hashing.Hash_AES_ALLOC()

	if bytes.Equal(hashing.Hash_AES1([]byte(orig), len(orig), true, ctx), hashing.Hash([]byte(orig), len(orig), false)) == true {
		fmt.Fprintf(os.Stderr, "aes_noprefetch works\n\n")
	} else {
		fmt.Fprintf(os.Stderr, "aes_noprefetch DIFFERENT\n\n")
	}
	hashing.Hash_AES_FREE(ctx)
	ctx = hashing.Hash_AES_ALLOC()

	if bytes.Equal(hashing.Hash_AES2([]byte(orig), len(orig), true, ctx), hashing.Hash([]byte(orig), len(orig), false)) == true {
		fmt.Fprintf(os.Stderr, "softaes_prefetch works\n\n")
	} else {
		fmt.Fprintf(os.Stderr, "softaes_prefetch DIFFERENT\n\n")
	}
	hashing.Hash_AES_FREE(ctx)
	ctx = hashing.Hash_AES_ALLOC()

	if bytes.Equal(hashing.Hash_AES3([]byte(orig), len(orig), true, ctx), hashing.Hash([]byte(orig), len(orig), false)) == true {
		fmt.Fprintf(os.Stderr, "softaes_noprefetch works\n\n")
	} else {
		fmt.Fprintf(os.Stderr, "softaes_noprefetch DIFFERENT\n\n").
	}
	hashing.Hash_AES_FREE(ctx)
	{
		/*
			ctx := hashing.Hash_AES_ALLOC()
			start := time.Now()
			for i := 0; i < 1000; i++ {
				hashing.Hash_AES([]byte("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"), 76, true, ctx)
			}
			fmt.Printf("VAR0: %f h/s\n\n", 1000/(float64(time.Since(start))/float64(time.Second)))
			hashing.Hash_AES_FREE(ctx)
			ctx = hashing.Hash_AES_ALLOC()
			start = time.Now()
			for i := 0; i < 1000; i++ {
				hashing.Hash_AES1([]byte("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"), 76, true, ctx)
			}
			fmt.Printf("VAR1: %f h/s\n\n", 1000/(float64(time.Since(start))/float64(time.Second)))
			hashing.Hash_AES_FREE(ctx)
			ctx = hashing.Hash_AES_ALLOC()
			start = time.Now()
			for i := 0; i < 1000; i++ {
				hashing.Hash_AES2([]byte("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"), 76, true, ctx)
			}
			fmt.Printf("VAR2: %f h/s\n\n", 1000/(float64(time.Since(start))/float64(time.Second)))
			hashing.Hash_AES_FREE(ctx)
			ctx = hashing.Hash_AES_ALLOC()
			start = time.Now()
			for i := 0; i < 1000; i++ {
				hashing.Hash_AES3([]byte("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"), 76, true, ctx)
			}
			fmt.Printf("VAR3: %f h/s\n\n", 1000/(float64(time.Since(start))/float64(time.Second)))
			hashing.Hash_AES_FREE(ctx)

			start = time.Now()
			for i := 0; i < 1000; i++ {
				hashing.Hash([]byte("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"), 76, false)
			}
			fmt.Printf("VAR_OLD: %f h/s\n\n", 1000/(float64(time.Since(start))/float64(time.Second)))
		*/
		/*
			ctx := hashing.Hash_AES_ALLOC()

			start := time.Now()
			for i := 0; i < 200; i++ {
				hashing.Hash_AES_5times([]byte("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"), 76*5, true, ctx)
			}
			fmt.Printf("VAR_5x: %f h/s\n\n", 1000/(float64(time.Since(start))/float64(time.Second)))
			hashing.Hash_AES_FREE(ctx)
		*/
	}

	flag.Parse()

	enc := json.NewEncoder(os.Stdout)
	os.Stdout.Sync()
	enc.SetEscapeHTML(false)

	result_chan := make(chan map[string]interface{})
	running_chan := make(chan bool, *num_concurrent)

	stdin_chan := make(chan string, 1)
	go line_from_stdin(stdin_chan, running_chan)

	running := true
	var hashes uint64 = 0

	start := time.Now()
	for len(running_chan) > 0 || running {
		select {
		case line, ok := <-stdin_chan:
			if ok {
				//do new job
				var inline map[string]interface{}
				if err := json.Unmarshal([]byte(line), &inline); err != nil {
					//log.Printf("Error parsing json: %s (%s)\n", err.Error(), line)
					break
				}
				go gen_token_for_item(inline["url"].(string), inline["postfix"].(string), inline["token"].(string), int(inline["target"].(float64)), result_chan, &hashes)
			} else {
				running = false
			}

		case <-time.After(500 * time.Millisecond):
			num_hashes := atomic.LoadUint64(&hashes)

			fmt.Fprintf(os.Stderr, "\rHashrate: %f h/s", float64(num_hashes)/(float64(time.Since(start))/float64(time.Second)))

		case result := <-result_chan:
			enc.Encode(result)

			<-running_chan
		}
	}
}

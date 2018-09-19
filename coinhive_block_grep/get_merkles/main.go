package main

import (
	"bytes"
	"encoding/hex"
	"flag"

	"math"

	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"

	"net/http"
)
import "github.com/steakknife/keccak"

type JobParams struct {
	Job_id string `json:"job_id"`
	Blob   string `json:"blob"`
	Target string `json:"target"`
}

type Job struct {
	Type     string       `json:"type"`
	Params   JobParams    `json:"params"`
	PoWBlock Mining_block `json:"pow"`
}

type Mining_block struct {
	Major            uint8  `json:"major"`
	Minor            uint8  `json:"minor"`
	Timestamp        uint64 `json:"timestamp"`
	previous         [32]byte
	Previous         string `json:"previous"`
	Nonce            uint32 `json:"nonce"`
	merkel_tree_root [32]byte
	Merkel_tree_root string `json:"merkel_tree_root"`
	Num_tx           uint64 `json:"num_tx"`
}

type Basic_block_info struct {
	Hash          string
	Difficulty    int
	Miner_tx_hash string
	Tx_hashes     []string
	Reward        int
}

type Output_info struct {
	Hash         string `json:"hash"`
	Difficulty   int    `json:"difficulty"`
	Merkle_root  string `json:"merkle_tree_root"`
	Block_reward int    `json:"block_reward"`
}

func get_block_info_for_height(height int, url string) *Basic_block_info {
	url = fmt.Sprintf("http://%s/json_rpc", url)

	var jsonStr = []byte(fmt.Sprintf("{\"jsonrpc\":\"2.0\",\"id\":\"0\",\"method\":\"getblock\",\"params\":{\"height\":%d}}", height))
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonStr))

	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	body, _ := ioutil.ReadAll(resp.Body)
	var response map[string]interface{}
	if err := json.Unmarshal(body, &response); err != nil {
		fmt.Fprintf(os.Stderr, "Failed decoding json: %s", err.Error())
	}
	if val, ok := response["error"]; ok && val != nil {
		return nil
	}

	result := response["result"].(map[string]interface{})
	//log.Printf("Response: %s", result)
	block_header := result["block_header"].(map[string]interface{})
	var Tx_hashes []string
	if result["tx_hashes"] != nil {
		for _, v := range result["tx_hashes"].([]interface{}) {
			Tx_hashes = append(Tx_hashes, v.(string))
		}
	} else {
		Tx_hashes = []string{}
	}
	return &Basic_block_info{Hash: block_header["hash"].(string), Difficulty: int(block_header["difficulty"].(float64)), Miner_tx_hash: result["miner_tx_hash"].(string), Tx_hashes: Tx_hashes, Reward: int(block_header["reward"].(float64))}
}

func cn_fast_hash_two(a []byte, b []byte) []byte {
	h := keccak.New256()
	h.Write(a)
	h.Write(b)
	return h.Sum(nil)
}
func compute_merkle_tree_root(hashes [][]byte) []byte {
	count := len(hashes)
	if count == 1 {
		return hashes[0]
	} else if count == 2 {
		return cn_fast_hash_two(hashes[0], hashes[1])
	}

	cnt := 1 << uint(math.Floor(math.Log2(float64(count))))

	ints := make([][]byte, cnt)

	for i := 0; i < 2*cnt-count; i += 1 {
		ints[i] = hashes[i]
	}

	for i, j := 2*cnt-count, 2*cnt-count; j < cnt; i, j = i+2, j+1 {
		ints[j] = cn_fast_hash_two(hashes[i], hashes[i+1])
	}

	for cnt > 2 {
		cnt >>= 1
		for i, j := 0, 0; j < cnt; i, j = i+2, j+1 {
			ints[j] = cn_fast_hash_two(ints[i], ints[i+1])
		}
	}
	return cn_fast_hash_two(ints[0], ints[1])
}

var start_height = flag.Int("start_height", 0, "start_heigth")
var url = flag.String("monero-node", "localhost:18081", "ip:port of monero node")

func main() {

	flag.Parse()
	enc := json.NewEncoder(os.Stdout)
	os.Stdout.Sync()
	enc.SetEscapeHTML(false)

	start := *start_height

	block := get_block_info_for_height(start, *url)
	for block != nil {
		//fmt.Printf("%s\n", block.Hash)
		var hashes [][]byte
		h, _ := hex.DecodeString(block.Miner_tx_hash)
		hashes = append(hashes, h)
		for _, val := range block.Tx_hashes {
			h, _ := hex.DecodeString(val)
			hashes = append(hashes, h)
		}
		root := compute_merkle_tree_root(hashes)

		enc.Encode(Output_info{Hash: block.Hash, Difficulty: block.Difficulty, Block_reward: block.Reward, Merkle_root: hex.EncodeToString(root)})

		start += 1

		block = get_block_info_for_height(start, *url)
	}
}

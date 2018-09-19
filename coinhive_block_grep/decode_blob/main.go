package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"time"
)

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
	merkle_tree_root [32]byte
	Merkle_tree_root string `json:"merkle_tree_root"`
	Num_tx           uint64 `json:"num_tx"`
}

func (b Mining_block) String() string {
	return fmt.Sprintf("Maj: %d\nMin: %d\nTimestamp: %s\nPrevious: %s\nNonce: %d\nMerkle Root: %s\nNum TX: %d\n", b.Major, b.Minor, time.Unix(int64(b.Timestamp), 0), b.Previous, b.Nonce, b.Merkle_tree_root, b.Num_tx)
}

func decode_varint(varint []byte) ([]byte, int) {
	output := make([]byte, 20)
	bytes_consumed := 0
	max_bits := uint(256)
	current_output_bit := uint(0)
	current_output_byte := uint(0)
	highest_one := uint(0)
	//fmt.Fprintf(os.Stderr, "max bits is %d %d\n", max_bits, len(varint))
	for bit := uint(0); bit < max_bits; bit++ {
		current_input_byte := bit / 8

		if (bit+1)%8 == 0 {
			// check if more bits are available
			// choose the correct byte
			bytes_consumed += 1
			if varint[current_input_byte]&(1<<7) == 0 {
				return output[:int(math.Ceil(float64(highest_one)/8.0))], bytes_consumed
			}
			continue
		}
		//fmt.Fprintf(os.Stderr, "Current input_bit: %d, output_bit %d value: %d\n", bit, current_output_bit, varint[current_output_byte]&(1<<(bit%8))>>(bit%8))
		current_output_byte = current_output_bit / 8
		if varint[current_input_byte]&(1<<(bit%8)) > 0 {
			output[current_output_byte] |= (1 << (current_output_bit % 8))
			highest_one = current_output_bit
		}
		current_output_bit += 1
	}
	return nil, 0
}

func decode_blob_to_mining_block(blob []byte) Mining_block {

	// fix the blob sneaky coinhive fuckers
	x := uint32(0)

	//uint32(blob[8:8+4]) = uint32(blob[8:8+4]) ^ 3768469278
	tmp := bytes.NewReader(blob[8 : 8+4])
	buf := new(bytes.Buffer)
	binary.Read(tmp, binary.LittleEndian, &x)
	// magic from wasm
	x = x ^ 3768469278

	binary.Write(buf, binary.LittleEndian, x)
	copy(blob[8:], buf.Bytes())

	b := Mining_block{}
	pos := 0

	output, consumed := decode_varint(blob)
	b.Major = output[0]
	pos += consumed

	output, consumed = decode_varint(blob[pos:])
	b.Minor = output[0]
	pos += consumed

	output, consumed = decode_varint(blob[pos:])
	b.Timestamp = 0
	for idx, val := range output {
		b.Timestamp |= uint64(val) << uint(8*(idx))
	}
	pos += consumed

	copy(b.previous[:], blob[pos:pos+32])

	b.Previous = hex.EncodeToString(b.previous[:])
	pos += 32

	b.Nonce = 0
	for idx, val := range blob[pos : pos+4] {
		b.Nonce |= uint32(val) << uint(8*(idx))
	}
	pos += 4

	copy(b.merkle_tree_root[:], blob[pos:pos+32])
	b.Merkle_tree_root = hex.EncodeToString(b.merkle_tree_root[:])
	pos += 32

	output, consumed = decode_varint(blob[pos:])
	b.Num_tx = 0
	for idx, val := range output {
		b.Num_tx |= uint64(val) << uint(8*(idx))
	}
	pos += consumed

	return b
}

func main() {
	fscanner := bufio.NewScanner(os.Stdin)
	maxsize := 64 * 1024 * 1024
	inbuff := make([]byte, maxsize, maxsize)
	fscanner.Buffer(inbuff, maxsize)

	enc := json.NewEncoder(os.Stdout)
	os.Stdout.Sync()
	enc.SetEscapeHTML(false)

	for fscanner.Scan() {
		var job Job
		if err := json.Unmarshal(fscanner.Bytes(), &job); err != nil {
			fmt.Fprintf(os.Stderr, "Error parsing json: %s (%s)\n", err.Error(), fscanner.Text())
			break
		}
		bin_blob := make([]byte, 120)
		hex.Decode(bin_blob, ([]byte)(job.Params.Blob))
		pow := decode_blob_to_mining_block(bin_blob)
		job.PoWBlock = pow
		enc.Encode(job)
	}
}

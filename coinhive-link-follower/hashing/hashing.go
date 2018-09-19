package hashing

// #cgo CFLAGS: -std=c11 -D_GNU_SOURCE -I/Users/jan/projects/monero/src -I/Users/jan/projects/monero/contrib/epee/include
// #cgo LDFLAGS: -lcncrypto
// #include <stdlib.h>
// #include <stdint.h>
// #include "crypto/hash-ops.h"
import "C"
import "unsafe"

func Hash(blob []byte, size int, fast bool) []byte {
	output := make([]byte, 32)
	if fast {
		C.cn_fast_hash(unsafe.Pointer((*C.char)(unsafe.Pointer(&blob[0]))), (C.size_t)(size), (*C.char)(unsafe.Pointer(&output[0])))
	} else {
		C.cn_slow_hash(unsafe.Pointer((*C.char)(unsafe.Pointer(&blob[0]))), (C.size_t)(size), (*C.char)(unsafe.Pointer(&output[0])), 1, 0)
	}
	return output
}

func FastHash(blob []byte, l byte) []byte {
	return Hash(append([]byte{l}, blob...), len(blob)+1, true)
}

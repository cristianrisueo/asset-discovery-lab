package labkit

import (
	"crypto/rand"
	"fmt"
	"math/big"

	"google.golang.org/protobuf/proto"
)

// NewUUID returns a random RFC 4122 version 4 UUID.
func NewUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// RandomBytes returns n random bytes.
func RandomBytes(n int) []byte {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return b
}

// UndecodableBytes returns random bytes, between minLen and maxLen long, that
// do not decode as msg. Random bytes occasionally form valid Protobuf wire
// data; those draws are rejected so that tests expecting DECODE_ERROR are
// deterministic.
func UndecodableBytes(minLen, maxLen int, msg proto.Message) []byte {
	for {
		n := minLen
		if maxLen > minLen {
			x, _ := rand.Int(rand.Reader, big.NewInt(int64(maxLen-minLen+1)))
			n += int(x.Int64())
		}
		b := RandomBytes(n)
		probe := msg.ProtoReflect().New().Interface()
		if proto.Unmarshal(b, probe) != nil {
			return b
		}
	}
}

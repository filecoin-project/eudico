package mir

import (
	"fmt"

	t "github.com/filecoin-project/mir/pkg/types"
)

type RequestType int

type Request interface{}

type RequestRef struct {
	ClientID t.ClientID
	ReqNo    t.ReqNo
	Type     RequestType
	Hash     []byte
}

func newMirID(subnet, addr string) string {
	return fmt.Sprintf("%s:%s", subnet, addr)
}

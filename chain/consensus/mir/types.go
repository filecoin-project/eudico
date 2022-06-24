package mir

import (
	"fmt"

	t "github.com/filecoin-project/mir/pkg/types"
)

type Request struct {
	ClientID t.ClientID
	ReqNo    t.ReqNo
	Data     []byte
}

type RequestData struct {
	ClientID t.ClientID
	ReqNo    t.ReqNo
	Data     []byte
}

func newMirID(subnet, addr string) string {
	return fmt.Sprintf("%s:%s", subnet, addr)
}

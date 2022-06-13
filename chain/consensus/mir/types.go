package mir

import t "github.com/filecoin-project/mir/pkg/types"

type RequestType int

type Request interface{}

type RequestRef struct {
	ClientID t.ClientID
	ReqNo    t.ReqNo
	Type     RequestType
	Hash     []byte
}

package mir

import (
	"fmt"
	"strings"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/lotus/chain/consensus/common"
	"github.com/filecoin-project/lotus/chain/types"
	t "github.com/filecoin-project/mir/pkg/types"
)

const (
	nodeBasePort = 10000
)

func splitAndTrimEmpty(s, sep, cutset string) []string {
	if s == "" {
		return []string{}
	}

	spl := strings.Split(s, sep)
	nonEmptyStrings := make([]string, 0, len(spl))

	for i := 0; i < len(spl); i++ {
		element := strings.Trim(spl[i], cutset)
		if element != "" {
			nonEmptyStrings = append(nonEmptyStrings, element)
		}
	}

	return nonEmptyStrings
}

// parsePersistentNodes parses comma-delimited ID@host:port persistent peers string.
// Example of the peers sting: "ID1@IP1:26656,ID2@IP2:26656,ID3@IP3:26656,ID4@IP4:26656".
// At present, we suppose that input is trusted.
// TODO: add input validation.
func parsePersistentNodes(input string) ([]t.NodeID, map[t.NodeID]string, error) {
	var nodeIds []t.NodeID
	nodeAddrs := make(map[t.NodeID]string)

	for _, idAddr := range splitAndTrimEmpty(input, ",", " ") {
		ss := strings.Split(idAddr, "@")
		if len(ss) != 2 {
			return nil, nil, xerrors.New("failed to parse persistent nodes")
		}

		id := t.NodeID(ss[0])
		netAddr := ss[1]
		nodeIds = append(nodeIds, id)
		nodeAddrs[id] = netAddr
	}
	return nodeIds, nodeAddrs, nil
}

// nolint
func getConfig(n int) ([]t.NodeID, map[t.NodeID]string) {
	var nodeIds []t.NodeID
	for i := 0; i < n; i++ {
		nodeIds = append(nodeIds, t.NewNodeIDFromInt(i))
	}

	nodeAddrs := make(map[t.NodeID]string)
	for i := range nodeIds {
		nodeAddrs[t.NewNodeIDFromInt(i)] = fmt.Sprintf("127.0.0.1:%d", nodeBasePort+i)
	}

	return nodeIds, nodeAddrs
}

// parseTx parses a raw byte transaction from Mir node into a Filecoin message.
func parseTx(tx []byte) (msg interface{}, err error) {
	ln := len(tx)
	if ln <= 2 {
		return nil, xerrors.Errorf("tx len %d is too small", ln)
	}

	lastByte := tx[ln-1]
	switch lastByte {
	case common.SignedMessageType:
		msg, err = types.DecodeSignedMessage(tx[:ln-1])
	case common.CrossMessageType:
		msg, err = types.DecodeUnverifiedCrossMessage(tx[:ln-1])
	default:
		err = xerrors.Errorf("unknown message type %d", lastByte)
	}

	return
}

// getMessagesFromMirBlock retrieves Filecoin messages from a Mir block.
func getMessagesFromMirBlock(b []Tx) (msgs []*types.SignedMessage, crossMsgs []*types.Message) {
	for _, tx := range b {
		msg, err := parseTx(tx)
		if err != nil {
			log.Error("unable to parse a message from Mir block:", err)
			continue
		}

		switch m := msg.(type) {
		case *types.SignedMessage:
			msgs = append(msgs, m)
		case *types.UnverifiedCrossMsg:
			crossMsgs = append(crossMsgs, m.Msg)
		default:
			log.Error("received an unknown message in Mir block")
		}
	}
	return
}

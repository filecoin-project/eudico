package subnet

import (
	"context"
	"fmt"
	"strings"

	"golang.org/x/xerrors"

	t "github.com/filecoin-project/mir/pkg/types"
)

type contextKey string

const validatorKey = contextKey("validator")

// ValAddress encapsulated string to be compatible with CBOR implementation.
type ValAddress struct {
	Value string
}

// EncodeValInfo adds a validator ID and address into a string.
func EncodeValInfo(vals map[string]ValAddress) string {
	var s string
	for i, addr := range vals {
		s += fmt.Sprintf("%s@%s,", i, addr.Value)
	}
	return strings.TrimSuffix(s, ",")
}

// SetVal adds validator addresses into the context.
func SetVal(ctx context.Context, v string) context.Context {
	return context.WithValue(ctx, validatorKey, v)
}

// ValStringFromContext returns validator addresses from the context.
func ValStringFromContext(ctx context.Context) (string, error) {
	v, ok := ctx.Value(validatorKey).(string)
	if !ok {
		return "", xerrors.New("claim value missing from context")
	}
	return v, nil
}

// ParseValInfo parses comma-delimited ID@host:port persistent validator string.
// Example of the peers sting: "ID1@IP1:26656,ID2@IP2:26656,ID3@IP3:26656,ID4@IP4:26656".
// At present, we suppose that input is trusted.
// TODO: add input validation.
func ParseValInfo(input string) ([]t.NodeID, map[t.NodeID]string, error) {
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

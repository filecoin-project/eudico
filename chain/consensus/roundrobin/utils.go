package roundrobin

import (
	"fmt"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
)

// validatorsFromString parses comma-separated validator addresses string.
//
// Examples of the validator string: "t1wpixt5mihkj75lfhrnaa6v56n27epvlgwparujy,t1wpixt5mihkj75lfhrnaa6v56n27epvlgwparujy"
func validatorsFromString(input string) ([]address.Address, error) {
	var addrs []address.Address
	for _, id := range hierarchical.SplitAndTrimEmpty(input, ",", " ") {
		a, err := address.NewFromString(id)
		if err != nil {
			return nil, fmt.Errorf("failed to parse %v: %w", id, err)
		}
		addrs = append(addrs, a)
	}
	return addrs, nil
}

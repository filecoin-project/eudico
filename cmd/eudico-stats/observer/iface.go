package observer

import (
	logging "github.com/ipfs/go-log/v2"
)

var log = logging.Logger("console-observer")

const MetricSubnets = "subnets"

type Observer interface {
	Observe(metric string, value ...interface{})
}

package observer

type BasicObserver struct{}

func (n *BasicObserver) Observe(metric string, values ...interface{}) {
	switch metric {
	case MetricSubnets:
		n.observeSubnets(values[0].(string), values[1].([]string))
	default:
		return
	}
	log.Infow("observed values", "metric", metric, "values", values)
}

func (n *BasicObserver) observeSubnets(id string, subnets []string) {
	for _, subnetID := range subnets {
		log.Infow("parent and subnet relationship", "id", id, "subnet", subnetID)
	}
}

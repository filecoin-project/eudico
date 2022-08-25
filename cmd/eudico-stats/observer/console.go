package observer

type ConsolePrint struct{}

func (n *ConsolePrint) Observe(metric string, values ...interface{}) {
	log.Infow("observed values", "metric", metric, "values", values)
}

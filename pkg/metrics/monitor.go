package metrics

type IMetricsMonitor interface {
	Shutdown() error
}

type MetricsMonitor struct{}

func NewMetricsMonitor() *MetricsMonitor {
	return &MetricsMonitor{}
}

func (m *MetricsMonitor) Shutdown() error {
	return nil
}

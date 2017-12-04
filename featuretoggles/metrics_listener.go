package featuretoggles

import unleash "github.com/Unleash/unleash-client-go"

type MetricsListener struct {
}

func (m *MetricsListener) OnCount(string, bool) {

}

func (m *MetricsListener) OnSent(unleash.MetricsData) {

}

func (m *MetricsListener) OnRegistered(unleash.ClientData) {

}

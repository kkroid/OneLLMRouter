package main

type healthPayload struct {
	Status      string `json:"status"`
	Service     string `json:"service"`
	PID         int    `json:"pid"`
	Version     string `json:"version"`
	HTTPPort    int    `json:"http_port"`
	Models      int    `json:"models"`
	ProxySOCKS5 string `json:"proxy_socks5"`
}

func buildHealthPayload(version string, pid, port, models int, proxyAddress string) healthPayload {
	return healthPayload{
		Status:      "ok",
		Service:     "onellm-router",
		PID:         pid,
		Version:     version,
		HTTPPort:    port,
		Models:      models,
		ProxySOCKS5: proxyAddress,
	}
}

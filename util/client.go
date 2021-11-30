package util

import (
	"io"
	"net/http"
	"time"

	"github.com/indes/flowerss-bot/config"
	"go.uber.org/zap"
	"golang.org/x/net/proxy"
)

var (
	HttpClient *http.Client
)

func clientInit() {
	httpTransport := &http.Transport{}
	HttpClient = &http.Client{Transport: httpTransport, Timeout: 15 * time.Second}
	// set proxy
	if config.Socks5 != "" {
		zap.S().Infow("enable proxy",
			"socks5", config.Socks5,
		)

		dialer, err := proxy.SOCKS5("tcp", config.Socks5, nil, proxy.Direct)
		if err != nil {
			zap.S().Fatal("Error creating dialer, aborting.")
		}
		httpTransport.Dial = dialer.Dial
	}
}

func SendRequest(req *http.Request) (*http.Response, error) {
	resp, err := HttpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)
	return resp, err
}

package http

import (
	"context"
	"io"
	"net/http"
	"net/http/httptrace"
	"time"
)

// TimeTrack  time track of the request
type TimeTrack struct {
	StartTime time.Time
	DNSTime   time.Time
	// Connect or get a connection from the pool
	ConnTime time.Time

	RequestTime   time.Time
	FirstByteTime time.Time
	EndTime       time.Time
}

// Request client: 对java服务不需要， 对php可能需要先认证
func Request(ctx context.Context, client *http.Client, method, url string, header map[string]string, body io.Reader) (*http.Response, *TimeTrack, error) {

	var tracetime TimeTrack

	ct := httptrace.ClientTrace{
		GotConn: func(info httptrace.GotConnInfo) {
			tracetime.ConnTime = time.Now()
		},
		WroteRequest: func(info httptrace.WroteRequestInfo) {
			tracetime.RequestTime = time.Now()
		},
		GotFirstResponseByte: func() {
			tracetime.FirstByteTime = time.Now()
		},
		DNSDone: func(info httptrace.DNSDoneInfo) {
			tracetime.DNSTime = time.Now()
		},
	}

	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, nil, err
	}
	if header != nil {
		for k, v := range header {
			req.Header.Set(k, v)
		}
	}

	if ctx == nil {
		var cf context.CancelFunc
		ctx, cf = context.WithTimeout(context.Background(), 10*time.Second)
		defer cf()
	}

	if client == nil {
		client = http.DefaultClient
	}

	traceCtx := httptrace.WithClientTrace(ctx, &ct)

	req = req.WithContext(traceCtx)
	tracetime.StartTime = time.Now()
	resp, err := client.Do(req)
	tracetime.EndTime = time.Now()

	return resp, &tracetime, err
}

package http

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"time"

	"golang.org/x/net/context"
	"golang.org/x/net/context/ctxhttp"
)

const contentTypeJSON = "application/json"

// DialTest a service, with service and interface set in the header
func DialTest(host, port string, header map[string]string, req string, timeout time.Duration) (output string, err error) {
	var reqJSON map[string]interface{}

	err = json.Unmarshal(([]byte)(req), &reqJSON)
	if err != nil {
		return
	}

	var buf bytes.Buffer
	if err = json.NewEncoder(&buf).Encode(reqJSON); err != nil {
		return
	}

	url := fmt.Sprintf("http://%s:%s", host, port)
	ctx := context.Background()
	var cancel context.CancelFunc
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	ctx, cancel = context.WithTimeout(ctx, timeout)
	defer cancel()

	var resp *http.Response
	resp, err = post(ctx, http.DefaultClient, url, header, contentTypeJSON, &buf)

	if err != nil {
		return
	}

	var out []byte
	out, err = ioutil.ReadAll(resp.Body)
	resp.Body.Close()

	output = string(out)

	if resp.StatusCode/100 != 2 {
		err = fmt.Errorf("unexpected status code %v from %s", resp.StatusCode, url)
	}
	return
}

func post(ctx context.Context, client *http.Client, url string, header map[string]string, bodyType string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequest("POST", url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", bodyType)
	if header != nil {
		for k, v := range header {
			req.Header.Set(k, v)
		}

	}
	return ctxhttp.Do(ctx, client, req)
}

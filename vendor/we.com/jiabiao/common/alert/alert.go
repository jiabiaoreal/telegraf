package alert

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"net/http"

	"github.com/golang/glog"
)

// SendMsg send an alert msg
func SendMsg(msg string) error {
	return nil
}

var (
	sendAlert = false
	AlertApi  = "http://alarm.we.com/api/v1/alerts"
)

// SendAlert set whether send alerts
func SendAlert(send bool) {
	sendAlert = send
}

// Message alert entity
type Message struct {
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
}

// SendAlerts send alerts
func SendAlerts(messages ...Message) error {

	glog.Infof("send %v alerts: %T", len(messages), messages)

	if !sendAlert {
		return nil
	}
	buf, err := json.Marshal(messages)

	reader := bytes.NewReader(buf)
	resp, err := http.Post(AlertApi, "Content-type: application/json", reader)
	if err != nil {
		glog.Warningf("send alert failed: %s", err)
	}

	if resp != nil {
		content, _ := ioutil.ReadAll(resp.Body)
		glog.Infof("send alert status code: %s, response content: %q", resp.Status, content)
		resp.Body.Close()
	}

	return err
}

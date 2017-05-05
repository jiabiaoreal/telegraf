package dial

import (
	"bytes"
	"encoding/json"
	"io"
	"io/ioutil"
	"net/http"
	"time"

	"golang.org/x/net/context"
	"golang.org/x/net/context/ctxhttp"
)

//Drequest is a dial request messages
type Drequest struct {
	Header  map[string]string
	Body    []byte
	Method  string
	Model   string //model is rpc or platform
	URL     string
	Timeout time.Duration
}

/*
check:
	http
		json:
			sucess:
			structure
		html:
			contain: content
			size:  >2 < 3
			status: 200
		binary
			size:
			md5sum:
			status:

	exec:
		exit
		content:

*/

//Dresponse is dialtest result
type Dresponse struct {
	Header map[string]string
	Body   string
	// elapsed 数据可以更完善一些：参考 import "net/http/httptrace"
	Elapsed time.Duration
	Status  bool
	Result  string
}

//Dial a service, with service and interface set in the header
//method:GET POST PUT DELETE..
func Dial(dreq *Drequest) (dresp *Dresponse, err error) {
	var reqJSON map[string]interface{}
	dresp = &Dresponse{}

	err = json.Unmarshal(dreq.Body, &reqJSON)
	if err != nil {
		return
	}

	var buf bytes.Buffer
	if err = json.NewEncoder(&buf).Encode(reqJSON); err != nil {
		return
	}

	ctx := context.Background()
	var cancel context.CancelFunc
	if dreq.Timeout == 0 {
		dreq.Timeout = 5 * time.Second
	}
	ctx, cancel = context.WithTimeout(ctx, dreq.Timeout)
	defer cancel()

	var resp *http.Response
	beggintime := time.Now()
	resp, err = request(ctx, http.DefaultClient, dreq.Method, dreq.URL, dreq.Header, &buf)
	dur := time.Since(beggintime)

	//fmt.Println(dur)
	if err != nil {
		return
	}

	defer resp.Body.Close()

	var out []byte
	out, err = ioutil.ReadAll(resp.Body)

	var result string
	if len(out) > 10240 {
		result = string(out[:10240])
	} else {
		result = string(out)
	}

	dresp.Header = dreq.Header
	dresp.Body = string(dreq.Body)
	dresp.Elapsed = dur
	dresp.Result = result

	respstatus, err := statuschk(dreq.Model, resp.StatusCode, string(out))
	if err != nil {
		return
	}
	dresp.Status = respstatus

	return
}

// client: 对java服务不需要， 对php可能老板娘先认证
func request(ctx context.Context, client *http.Client, method, url string, header map[string]string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}

	if header != nil {
		for k, v := range header {
			req.Header.Set(k, v)
		}
	}
	return ctxhttp.Do(ctx, client, req)
}

func report() error {
	//TODO
	//collect agent unique info  [耗时、服务、接口、拨测参数、拨测结果]

	//report to file or other
	return nil
}

type rest struct {
	Fail   string `json:"fail"`
	Err    string `json:"error"`
	Status int    `json:"status"`
}

// 具体怎么检测返回的结果，可以在调用的dial的时候以option的形式指定
// WithResultContains, WithResultSize, WitherResultMd5sum， WithHttpClient 等
func statuschk(model string, code int, response string) (bool, error) {
	var result rest
	//var status int
	if err := json.Unmarshal([]byte(response), &result); err != nil {
		//fmt.Println(part.Status)
		//status = result.Status
		return false, err
	}

	if code == 200 {
		if model == "platform" {
			/*if strings.Contains(response, "fail") {
				return false, nil
			}*/
			if result.Fail == "" {
				return true, nil
			}
		} else if model == "rpc" {
			if result.Err == "" {
				return true, nil
			}
		}
	}

	return false, nil
}

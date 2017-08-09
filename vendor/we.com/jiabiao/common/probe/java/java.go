package java

import (
	"context"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/pkg/errors"
	"we.com/jiabiao/common/probe"
	phttp "we.com/jiabiao/common/probe/http"
	"we.com/jiabiao/common/yaml"
)

const (
	// DefaultRespSize max  bytes read from response for an probe
	DefaultRespSize = 1024 * 1024
)

// Args args config when probe
type Args struct {
	Name        string
	Cluster     string
	URL         string
	Headers     map[string]string
	Data        io.Reader
	MaxRespSize int64
}

// Probe implements probe.Prober interface
func Probe(lg probe.LoadGenerator) (probe.Result, string, error) {
	dat := lg()
	if dat == nil {
		return probe.Failure, "", errors.New("java probe: cannot get args from load generator")
	}

	args, ok := dat.([]*Args)
	if !ok {
		return probe.Failure, "", errors.New("java probe: load generator must return data of type Java Args")
	}

	result := probe0(args)

	for _, r := range result {
		if r.err != nil {
			return probe.Failure, "", r.err
		}
		if r.Result == probe.Failure {
			return probe.Failure, "", r.err
		}
	}

	return probe.Success, "", nil
}

// Result java probe result
type Result struct {
	Name      string
	Result    probe.Result
	data      string
	TimeTrack *phttp.TimeTrack
	err       error
}

func probe0(args []*Args) map[string]*Result {
	if len(args) == 0 {
		return nil
	}
	ctx, cf := context.WithCancel(context.Background())
	time.AfterFunc(10*time.Second, cf)

	resultC := make(chan *Result, 1)
	defer close(resultC)
	var wg sync.WaitGroup

	p := func(args *Args) {
		wg.Add(1)
		defer wg.Done()
		ps := Result{
			Name:   args.Name,
			Result: probe.Failure,
		}
		resp, tt, err := phttp.Request(ctx, nil, "POST", args.URL, args.Headers, args.Data)
		ps.err = err
		ps.TimeTrack = tt
		if err != nil {
			resultC <- &ps
			return
		}

		if args.MaxRespSize <= 0 {
			args.MaxRespSize = DefaultRespSize
		}

		size := resp.ContentLength
		// read at most args.MaxRespSize data from response
		if size > args.MaxRespSize {
			size = args.MaxRespSize
			ps.err = errors.Errorf("response to large: %v", size)
		}

		content := make([]byte, size)
		_, err = io.ReadFull(resp.Body, content)
		ps.data = string(content)
		if err != nil {
			ps.err = err
			return
		}

		if ps.err != nil {
			ps.Result = checkResp(ps.data)
		}

		resultC <- &ps
	}

	ret := map[string]*Result{}

	go func() {
		for {
			select {
			case r, ok := <-resultC:
				if !ok {
					return
				}
				ret[r.Name] = r
			}
		}
	}()

	for _, a := range args {
		go p(a)
	}

	wg.Wait()

	return ret
}

type respStatus struct {
	Status  int    `json:"status,omitempty"`
	Success string `json:"success,omitempty"`
	Fail    string `json:"fail,omitempty"`
}

func checkResp(res string) probe.Result {
	input := strings.NewReader(res)
	decoder := yaml.NewYAMLOrJSONDecoder(input, 4)
	result := respStatus{}
	err := decoder.Decode(&result)
	if err != nil {
		return probe.Failure
	}

	if result.Status > 300 {
		return probe.Failure
	}

	if result.Fail != "" {
		return probe.Failure
	}

	return probe.Failure
}

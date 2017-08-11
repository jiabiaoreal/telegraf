package java

import (
	"context"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/golang/glog"
	"github.com/pkg/errors"
	"we.com/jiabiao/common/probe"
	phttp "we.com/jiabiao/common/probe/http"
	"we.com/jiabiao/common/yaml"

	"github.com/hashicorp/go-multierror"
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

	if len(args) == 0 {
		glog.V(15).Infof("probe args is empty, return")
		return probe.Success, "", nil
	}

	glog.V(20).Infof("probe: start %v, %+v", len(args), args[0])
	result := probe0(args)

	count := 0
	var merr *multierror.Error
	for _, r := range result {
		if r.err != nil || r.Result == probe.Failure {
			count++
			if r.err != nil {
				merr = multierror.Append(merr, r.err)
			}
		}
	}

	if count*2 > len(args) {
		return probe.Failure, "", merr.ErrorOrNil()
	}
	if count > 0 {
		return probe.Warning, "", merr.ErrorOrNil()
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
		if size <= 0 {
			size = args.MaxRespSize
		}

		content := make([]byte, size)
		s, err := io.ReadFull(resp.Body, content)
		ps.data = string(content)
		if err != nil {
			ps.err = err
			return
		}

		if ps.err != nil {
			if int64(s) >= args.MaxRespSize {
				ps.err = errors.Errorf("response to large: %v", size)
			}
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
		wg.Add(1)
		go p(a)
	}

	wg.Wait()

	return ret
}

type respStatus struct {
	Status int    `json:"status,omitempty"`
	Fail   string `json:"fail,omitempty"`
	Error  string `json:"error,omitempty"`
}

func checkResp(res string) probe.Result {
	input := strings.NewReader(res)
	decoder := yaml.NewYAMLOrJSONDecoder(input, 4)
	result := respStatus{}
	err := decoder.Decode(&result)
	if err != nil {
		glog.Warningf("probe: decode response: %v, %v", err, res)
		return probe.Failure
	}
	glog.V(15).Infof("probe: result: %v", result)

	if result.Status > 300 {
		return probe.Failure
	}

	if result.Fail != "" {
		return probe.Failure
	}

	return probe.Failure
}

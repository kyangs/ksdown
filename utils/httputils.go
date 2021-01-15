package utils

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"time"
)

type (
	HttpMethod string
)

var (
	HttpClient = &http.Client{
		Timeout: 0 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
)

const (
	HttpMethodPost   HttpMethod = "POST"
	HttpMethodGet    HttpMethod = "GET"
	HttpMethodDelete HttpMethod = "DELETE"

	HttpStatus200             string = "200 OK"
	HttpResponseSuccessStatus string = "SUCCESS"
	HttpResponseFailStatus    string = "FAIL"
)

func HttpRequest(method HttpMethod, url string, param interface{}, header map[string]string) (*http.Response, error) {

	bs, err := json.Marshal(param)

	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(string(method), url, bytes.NewBuffer(bs))
	if err != nil {
		return nil, err
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}

	resp, err := HttpClient.Do(req)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

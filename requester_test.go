package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"testing"
	"time"
)

func Test_probe(t *testing.T) {
	webContent := "Hello World!"
	singleServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, webContent)
	}))
	defer singleServer.Close()

	multipartServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqData, _ := httputil.DumpRequest(r, false)
		t.Log(string(reqData))

		content := &ContentBuffer{
			Data:  []byte(webContent),
			Index: 0,
		}
		http.ServeContent(w, r, "sample.txt", time.Now(), content)
	}))
	defer multipartServer.Close()

	type args struct {
		url string
	}
	tests := []struct {
		name       string
		args       args
		wantLength int64
		wantErr    bool
	}{
		{"SingleServer", args{url: singleServer.URL}, 0, false},
		{"MultipartServer", args{url: multipartServer.URL}, 12, false},
		{"InvalidServer", args{url: "invalid-url"}, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotLength, err := probe(tt.args.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("probe() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if gotLength != tt.wantLength {
				t.Errorf("probe() = %v, want %v", gotLength, tt.wantLength)
			}
		})
	}
}

package main

import (
	"testing"

	"github.com/artificerpi/oget/ogettest"
)

func Test_probe(t *testing.T) {
	simpleServer := ogettest.NewSimpleServer()
	defer simpleServer.Close()

	simpleRangeServer := ogettest.NewSimpleRangeServer()
	defer simpleRangeServer.Close()

	type args struct {
		url string
	}
	tests := []struct {
		name       string
		args       args
		wantLength int64
		wantErr    bool
	}{
		{"SingleServer", args{url: simpleServer.URL}, 0, false},
		{"MultipartServer", args{url: simpleRangeServer.URL}, int64(len(ogettest.DefaultWebContent)), false},
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

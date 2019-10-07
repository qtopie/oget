package main

import (
	"bytes"
	"testing"

	"github.com/artificerpi/oget/ogettest"
)

func TestFetcher_retrieveAll(t *testing.T) {
	server := ogettest.NewSimpleServer()
	defer server.Close()

	type fields struct {
		URL    string
		Pieces []RangeHeader
	}
	tests := []struct {
		name    string
		fields  fields
		wantN   int64
		wantW   string
		wantErr bool
	}{
		{"fetchAll_mock", fields{URL: server.URL}, 12, ogettest.DefaultWebContent, false},
		{"fetchAll_failed", fields{URL: "invalid-url"}, 0, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &Fetcher{
				URL:    tt.fields.URL,
				Pieces: tt.fields.Pieces,
			}
			w := &bytes.Buffer{}
			gotN, err := f.retrieveAll(w)
			if (err != nil) != tt.wantErr {
				t.Errorf("Fetcher.retrieveAll() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if gotN != tt.wantN {
				t.Errorf("Fetcher.retrieveAll() = %v, want %v", gotN, tt.wantN)
			}
			if gotW := w.String(); gotW != tt.wantW {
				t.Errorf("Fetcher.retrieveAll() = %v, want %v", gotW, tt.wantW)
			}
		})
	}
}

func TestFetcher_retrievePartial(t *testing.T) {
	server := ogettest.NewSimpleRangeServer()
	defer server.Close()

	type fields struct {
		URL    string
		Pieces []RangeHeader
	}
	type args struct {
		pieceN int
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantN   int
		wantW   string
		wantErr bool
	}{
		{"fetchPartial_mock", fields{server.URL, []RangeHeader{{5, 9}}}, args{0}, 5, "World", false},
		{"fetchPartial_mock", fields{"invalid-url", []RangeHeader{{5, 9}}}, args{0}, 0, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &Fetcher{
				URL:    tt.fields.URL,
				Pieces: tt.fields.Pieces,
			}
			w := &ogettest.RangeBuffer{}
			gotN, err := f.retrievePartial(tt.args.pieceN, w)
			if (err != nil) != tt.wantErr {
				t.Errorf("Fetcher.retrievePartial() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if gotN != tt.wantN {
				t.Errorf("Fetcher.retrievePartial() = %v, want %v", gotN, tt.wantN)
			}

			rangeHeader := tt.fields.Pieces[tt.args.pieceN]
			gotData := make([]byte, tt.wantN)
			_, err = w.ReadAt(gotData, rangeHeader.StartPos)
			if err != nil {
				t.Log(err)
			}
			if gotW := string(gotData); gotW != tt.wantW {
				t.Errorf("Fetcher.retrievePartial() = %v, want %v", gotW, tt.wantW)
			}
		})
	}
}

package main

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestDownload(t *testing.T) {
	wanted := "abc"
	fileName := "direct_download_test"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, wanted)
	}))
	defer server.Close()

	Download(server.URL, fileName)
	// cleanup
	defer delete(fileName)

	data, err := ioutil.ReadFile(fileName)
	if err != nil {
		t.Error(err)
	}
	actual := string(data)

	if 0 != strings.Compare(wanted, actual) {
		t.Errorf("Expected to be %s but instead got %s", wanted, actual)
	}
}

func delete(path string) error {
	return os.Remove(path)
}

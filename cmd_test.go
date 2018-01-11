package main

import "testing"

func TestCommand_execute(t *testing.T) {
	type fields struct {
		URL      string
		FileName string
	}
	tests := []struct {
		name   string
		fields fields
	}{
		{"real_test", fields{"http://ftp.uninett.no/linux/ubuntu-iso/artful/ubuntu-17.10-desktop-amd64.iso", "test"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &Command{
				URL:      tt.fields.URL,
				FileName: tt.fields.FileName,
			}
			cmd.execute()
		})
	}
}

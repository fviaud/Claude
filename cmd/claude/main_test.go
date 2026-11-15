package main

import "testing"

func TestListenAddr(t *testing.T) {
	tests := []struct {
		name, addr, port, want string
	}{
		{"default", "", "", ":8080"},
		{"PORT only", "", "10000", ":10000"},
		{"ADDR wins over PORT", "127.0.0.1:9000", "10000", "127.0.0.1:9000"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("ADDR", tt.addr)
			t.Setenv("PORT", tt.port)
			if got := listenAddr(); got != tt.want {
				t.Errorf("listenAddr() = %q, want %q", got, tt.want)
			}
		})
	}
}

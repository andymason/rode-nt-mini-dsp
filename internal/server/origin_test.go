package server

import (
	"net/http"
	"testing"
)

// The GUI has no password: whoever can open a WebSocket to it can drive the
// microphone. The origin check is the only thing stopping a page the user
// happens to be visiting from doing that, so it is worth pinning.
//
// Host is the address the browser dialled; Origin is the page doing the
// dialling. They match only when the GUI is calling itself. Note that
// localhost and 127.0.0.1 are different origins as far as a browser is
// concerned, which is why each case carries its own Host.
func TestCheckOrigin(t *testing.T) {
	tests := []struct {
		name   string
		host   string
		origin string
		want   bool
	}{
		{"the GUI's own page", "localhost:8080", "http://localhost:8080", true},
		{"the GUI opened by IP", "127.0.0.1:8080", "http://127.0.0.1:8080", true},
		{"no Origin at all (curl, not a browser)", "localhost:8080", "", true},

		{"another site entirely", "localhost:8080", "https://evil.example", false},
		{"a site naming us in its path", "localhost:8080", "https://evil.example/localhost:8080", false},
		{"a lookalike subdomain", "localhost:8080", "http://localhost.evil.example", false},
		{"another port on this machine", "localhost:8080", "http://localhost:9999", false},
		{"the IP page talking to the name", "localhost:8080", "http://127.0.0.1:8080", false},
		{"an unparseable Origin", "localhost:8080", "http://a b c", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, err := http.NewRequest(http.MethodGet, "http://"+tc.host+"/ws", nil)
			if err != nil {
				t.Fatal(err)
			}
			r.Host = tc.host
			if tc.origin != "" {
				r.Header.Set("Origin", tc.origin)
			}

			if got := upgrader.CheckOrigin(r); got != tc.want {
				t.Errorf("Host %q, Origin %q: got %v, want %v", tc.host, tc.origin, got, tc.want)
			}
		})
	}
}

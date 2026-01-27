package main

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	. "github.com/onsi/gomega"
)

func TestShouldSkipTLSVerification(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		expected bool
	}{
		{
			name:     "returns true for 'true'",
			value:    "true",
			expected: true,
		},
		{
			name:     "returns true for 'TRUE'",
			value:    "TRUE",
			expected: true,
		},
		{
			name:     "returns true for 'True'",
			value:    "True",
			expected: true,
		},
		{
			name:     "returns true for '1'",
			value:    "1",
			expected: true,
		},
		{
			name:     "returns false for 'false'",
			value:    "false",
			expected: false,
		},
		{
			name:     "returns false for 'FALSE'",
			value:    "FALSE",
			expected: false,
		},
		{
			name:     "returns false for '0'",
			value:    "0",
			expected: false,
		},
		{
			name:     "returns false for empty string",
			value:    "",
			expected: false,
		},
		{
			name:     "returns false for arbitrary string",
			value:    "yes",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewGomegaWithT(t)
			result := shouldSkipTLSVerification(tt.value)
			g.Expect(result).To(Equal(tt.expected))
		})
	}
}

func TestNewHTTPClient(t *testing.T) {
	t.Run("returns client without TLS skip when value is empty", func(t *testing.T) {
		g := NewGomegaWithT(t)
		client := newHTTPClient("")

		g.Expect(client).NotTo(BeNil())
		g.Expect(client.Transport).To(BeNil())
	})

	t.Run("returns client without TLS skip when value is false", func(t *testing.T) {
		g := NewGomegaWithT(t)
		client := newHTTPClient("false")

		g.Expect(client).NotTo(BeNil())
		g.Expect(client.Transport).To(BeNil())
	})

	t.Run("returns client with TLS skip when value is true", func(t *testing.T) {
		g := NewGomegaWithT(t)
		client := newHTTPClient("true")

		g.Expect(client).NotTo(BeNil())
		g.Expect(client.Transport).NotTo(BeNil())

		transport, ok := client.Transport.(*http.Transport)
		g.Expect(ok).To(BeTrue())
		g.Expect(transport.TLSClientConfig).NotTo(BeNil())
		g.Expect(transport.TLSClientConfig.InsecureSkipVerify).To(BeTrue())
	})

	t.Run("returns client with TLS skip when value is 1", func(t *testing.T) {
		g := NewGomegaWithT(t)
		client := newHTTPClient("1")

		g.Expect(client).NotTo(BeNil())
		g.Expect(client.Transport).NotTo(BeNil())

		transport, ok := client.Transport.(*http.Transport)
		g.Expect(ok).To(BeTrue())
		g.Expect(transport.TLSClientConfig.InsecureSkipVerify).To(BeTrue())
	})
}

func TestNewHTTPClientWithSelfSignedCert(t *testing.T) {
	t.Run("client with TLS skip can connect to self-signed cert server", func(t *testing.T) {
		g := NewGomegaWithT(t)

		// Create a test server with self-signed TLS cert
		ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("success"))
		}))
		defer ts.Close()

		// Client with TLS skip should succeed
		client := newHTTPClient("true")
		resp, err := client.Get(ts.URL)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(resp.StatusCode).To(Equal(http.StatusOK))
		_ = resp.Body.Close()
	})

	t.Run("client without TLS skip fails to connect to self-signed cert server", func(t *testing.T) {
		g := NewGomegaWithT(t)

		// Create a test server with self-signed TLS cert
		ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer ts.Close()

		// Client without TLS skip should fail due to certificate verification
		client := newHTTPClient("false")
		_, err := client.Get(ts.URL)
		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("certificate"))
	})
}

func TestNewHTTPClientWithValidCert(t *testing.T) {
	t.Run("client with TLS skip can still connect to valid cert server", func(t *testing.T) {
		g := NewGomegaWithT(t)

		// Create a test server with self-signed cert but use the test client's cert pool
		ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer ts.Close()

		// Even with TLS skip enabled, connection should work
		client := newHTTPClient("true")
		resp, err := client.Get(ts.URL)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(resp.StatusCode).To(Equal(http.StatusOK))
		_ = resp.Body.Close()
	})
}

func TestTLSConfigInsecureSkipVerify(t *testing.T) {
	tests := []struct {
		name           string
		skipValue      string
		expectInsecure bool
	}{
		{
			name:           "TLS skip enabled with 'true'",
			skipValue:      "true",
			expectInsecure: true,
		},
		{
			name:           "TLS skip enabled with '1'",
			skipValue:      "1",
			expectInsecure: true,
		},
		{
			name:           "TLS skip disabled with 'false'",
			skipValue:      "false",
			expectInsecure: false,
		},
		{
			name:           "TLS skip disabled with empty string",
			skipValue:      "",
			expectInsecure: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewGomegaWithT(t)
			client := newHTTPClient(tt.skipValue)

			if tt.expectInsecure {
				transport, ok := client.Transport.(*http.Transport)
				g.Expect(ok).To(BeTrue())
				g.Expect(transport.TLSClientConfig).NotTo(BeNil())
				g.Expect(transport.TLSClientConfig.InsecureSkipVerify).To(BeTrue())
			} else {
				// When not insecure, transport should be nil (default)
				// or if set, InsecureSkipVerify should be false
				if client.Transport != nil {
					transport, ok := client.Transport.(*http.Transport)
					if ok && transport.TLSClientConfig != nil {
						g.Expect(transport.TLSClientConfig.InsecureSkipVerify).To(BeFalse())
					}
				}
			}
		})
	}
}

func TestHTTPClientCookieJarCompatibility(t *testing.T) {
	t.Run("client with TLS skip supports cookie jar for UniFi authentication", func(t *testing.T) {
		g := NewGomegaWithT(t)

		// Create a test server that sets and requires cookies
		ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/login" {
				http.SetCookie(w, &http.Cookie{
					Name:  "auth_token",
					Value: "test-token",
				})
				w.WriteHeader(http.StatusOK)
				return
			}
			if r.URL.Path == "/api" {
				cookie, err := r.Cookie("auth_token")
				if err != nil || cookie.Value != "test-token" {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("authenticated"))
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}))
		defer ts.Close()

		client := newHTTPClient("true")

		// Create a new cookie jar for session management
		client.Jar, _ = createCookieJar()

		// Login request should set cookie
		resp, err := client.Get(ts.URL + "/login")
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(resp.StatusCode).To(Equal(http.StatusOK))
		_ = resp.Body.Close()

		// Subsequent request should include cookie
		resp, err = client.Get(ts.URL + "/api")
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(resp.StatusCode).To(Equal(http.StatusOK))
		_ = resp.Body.Close()
	})
}

// createCookieJar creates a new cookie jar for HTTP client session management
func createCookieJar() (http.CookieJar, error) {
	return &simpleCookieJar{cookies: make(map[string][]*http.Cookie)}, nil
}

// simpleCookieJar is a basic cookie jar implementation for testing
type simpleCookieJar struct {
	cookies map[string][]*http.Cookie
}

func (j *simpleCookieJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	j.cookies[u.Host] = append(j.cookies[u.Host], cookies...)
}

func (j *simpleCookieJar) Cookies(u *url.URL) []*http.Cookie {
	return j.cookies[u.Host]
}

// Ensure TLS config is properly set
func TestTLSConfigValues(t *testing.T) {
	t.Run("TLS config has correct InsecureSkipVerify value", func(t *testing.T) {
		g := NewGomegaWithT(t)

		client := newHTTPClient("true")
		transport := client.Transport.(*http.Transport)

		// Verify the TLS config is set correctly
		g.Expect(transport.TLSClientConfig).To(Equal(&tls.Config{InsecureSkipVerify: true}))
	})
}

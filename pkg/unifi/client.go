package unifi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strconv"

	"github.com/ryanmcafee/port-forwarding-controller/pkg/forwarding"
	"golang.org/x/net/publicsuffix"
)

// DeviceType represents the type of UniFi device being controlled.
type DeviceType string

const (
	// DeviceTypeStandard is for legacy UniFi Controller software.
	DeviceTypeStandard DeviceType = "standard"
	// DeviceTypeUDM is for UniFi Dream Machine and Dream Router devices.
	DeviceTypeUDM DeviceType = "udm"
)

type Client struct {
	HTTPClient    *http.Client
	ControllerURL string
	Username      string
	Password      string
	APIKey        string     // Preferred over username/password when set
	DeviceType    DeviceType // "standard" (default) or "udm" for UDM/UDR devices
}

type baseResponse struct {
	Meta meta `json:"meta"`
}
type meta struct {
	Msg string `json:"msg"`
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type listResponse struct {
	Data []listItem `json:"data"`
}
type listItem struct {
	ID   string `json:"_id"`
	Name string `json:"name"`
	Port string `json:"fwd_port"`
	IP   string `json:"fwd"`
	Src  string `json:"src"`
}

type createRequest struct {
	Name    string `json:"name"`
	Port    string `json:"fwd_port"`
	IP      string `json:"fwd"`
	DstPort string `json:"dst_port"`
	Enabled bool   `json:"enabled"`
	Proto   string `json:"proto"`
	Src     string `json:"src"`
}

func (c Client) ListAddresses(options map[string]string) ([]forwarding.Address, error) {
	listResp, err := c.list(options)
	if err != nil {
		return nil, err
	}

	addresses := []forwarding.Address{}
	for _, a := range listResp.Data {
		p, err := strconv.Atoi(a.Port)
		if err != nil {
			return nil, err
		}
		src := a.Src
		if src == "any" {
			src = ""
		}
		addresses = append(addresses, forwarding.Address{
			Name:        a.Name,
			Port:        p,
			IP:          a.IP,
			SourceRange: src,
			Options:     options,
		})
	}

	return addresses, nil
}

func (c Client) list(options map[string]string) (listResponse, error) {
	endpoint := c.portForwardEndpoint(c.siteName(options))
	respBody, err := c.makeAPICall("GET", endpoint, nil)
	if err != nil {
		return listResponse{}, err
	}

	var listResp listResponse
	err = json.Unmarshal(respBody, &listResp)
	if err != nil {
		return listResponse{}, err
	}
	return listResp, nil
}

func (c Client) siteName(options map[string]string) string {
	if name, ok := options["unifi-site"]; ok {
		return name
	}
	return "default"
}

// apiBasePath returns the base API path based on device type.
// UDM/UDR devices use /proxy/network/api, standard controllers use /api.
func (c Client) apiBasePath() string {
	if c.DeviceType == DeviceTypeUDM {
		return "/proxy/network/api"
	}
	return "/api"
}

// loginEndpoint returns the full login URL based on device type.
func (c Client) loginEndpoint() string {
	if c.DeviceType == DeviceTypeUDM {
		return fmt.Sprintf("%s/api/auth/login", c.ControllerURL)
	}
	return fmt.Sprintf("%s/api/login", c.ControllerURL)
}

// portForwardEndpoint returns the port forward API endpoint for the given site.
func (c Client) portForwardEndpoint(siteName string) string {
	return fmt.Sprintf("%s%s/s/%s/rest/portforward", c.ControllerURL, c.apiBasePath(), siteName)
}

// portForwardDeleteEndpoint returns the port forward delete endpoint for a specific rule.
func (c Client) portForwardDeleteEndpoint(siteName, id string) string {
	return fmt.Sprintf("%s%s/s/%s/rest/portforward/%s", c.ControllerURL, c.apiBasePath(), siteName, id)
}

func (c Client) CreateAddress(address forwarding.Address) error {
	src := "any"
	if address.SourceRange != "" {
		src = address.SourceRange
	}
	reqBody, err := json.Marshal(createRequest{
		Name:    address.Name,
		Port:    fmt.Sprintf("%d", address.Port),
		IP:      address.IP,
		DstPort: fmt.Sprintf("%d", address.Port),
		Enabled: true,
		Proto:   "tcp_udp",
		Src:     src,
	})
	if err != nil {
		return err
	}

	endpoint := c.portForwardEndpoint(c.siteName(address.Options))
	if _, err := c.makeAPICall("POST", endpoint, reqBody); err != nil {
		return err
	}

	return nil
}

func (c Client) DeleteAddress(address forwarding.Address) error {
	listResp, err := c.list(address.Options)
	if err != nil {
		return err
	}

	matchingID := ""
	for _, a := range listResp.Data {
		if a.Name == address.Name &&
			a.Port == fmt.Sprintf("%d", address.Port) &&
			a.IP == address.IP {
			matchingID = a.ID
			break
		}
	}

	if matchingID == "" {
		return nil
	}

	endpoint := c.portForwardDeleteEndpoint(c.siteName(address.Options), matchingID)
	_, err = c.makeAPICall("DELETE", endpoint, nil)
	if err != nil {
		return err
	}

	return nil
}

func (c Client) makeAPICall(method, endpoint string, body []byte) ([]byte, error) {
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, endpoint, bodyReader)
	if err != nil {
		return nil, err
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("X-Api-Key", c.APIKey)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	// Only handle login errors when NOT using API key
	if c.APIKey == "" && c.isLoginError(resp) {
		if err := c.login(); err != nil {
			return nil, err
		}
		return c.makeAPICall(method, endpoint, body)
	} else if resp.StatusCode >= 300 {
		return nil, c.buildRespErr(resp)
	}

	return io.ReadAll(resp.Body)
}

func (c Client) isLoginError(resp *http.Response) bool {
	if resp.StatusCode == 401 {
		var decodedResp baseResponse
		err := json.NewDecoder(resp.Body).Decode(&decodedResp)
		if err != nil {
			return false
		}
		if decodedResp.Meta.Msg == "api.err.LoginRequired" {
			return true
		}
	}
	return false
}

func (c Client) login() error {
	if c.HTTPClient.Jar == nil {
		jar, err := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
		if err != nil {
			return err
		}
		c.HTTPClient.Jar = jar
	}

	reqBody, err := json.Marshal(loginRequest{
		Username: c.Username,
		Password: c.Password,
	})
	if err != nil {
		return err
	}

	endpoint := c.loginEndpoint()
	resp, err := c.HTTPClient.Post(endpoint, "application/json", bytes.NewReader(reqBody))
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return c.buildRespErr(resp)
	}

	return nil
}

func (c Client) buildRespErr(resp *http.Response) error {
	if resp.Body == nil {
		return fmt.Errorf("invalid response code %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("invalid response code %d", resp.StatusCode)
	}

	return fmt.Errorf("invalid response code %d: %s", resp.StatusCode, string(body))
}

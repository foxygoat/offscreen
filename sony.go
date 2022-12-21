package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// RESTClient talks to a Sony Bravia TV using the [REST IP control protocol].
//
// The full API is not implemented, only just enough to power the set on and
// off based on a condition of an input being selected, and to switch inputs.
// This allows the display to be turned off if X11 XSS says the screen has been
// blanked, and that can be filtered to only turn if off if a particular input
// is selected. The screen should not be turned off if it is not displaying the
// screen that was just blanked. When XSS says a screen has been unblanked, if
// the screen is off, turn it on and select a particular input.
//
// [REST IP control protocol]: https://pro-bravia.sony.net/develop/integrate/rest-api/spec/index.html
type RESTClient struct {
	// BaseURL is the URL to address the TV set to be controlled. It will
	// typically be a hostname and the `/sony` path.
	BaseURL string

	// PSK is the Pre-Shared Key configured on the TV set. It is
	// essentially a password on the service (that is sent in plain-text on
	// the network).
	PSK string

	HTTPClient *http.Client
}

var (
	// ErrHTTPStatus is a sentinel error for all HTTP status-based errors, It is
	// intended to be used with `errors.Is(err, ErrHTTPStatus)`.
	ErrHTTPStatus = errors.New("http")

	// ErrSony is a sentinel error for errors returned by the REST IP
	// control protocol in the body of a response.
	ErrSony = errors.New("sony")
)

// HTTPStatusError captures the status code of a HTTP response that is to be
// treated as an error. It is not necessarily just a 4xx or 5xx error - it
// could be any status code that is unhandled.
type HTTPStatusError int

// Error formats a HTTP status code as its text description as per
// http.StatusText().
func (err HTTPStatusError) Error() string {
	return http.StatusText(int(err))
}

// Unwrap returns ErrHTTP for all errors so HTTPStatusErrors can be checked
// with `errors.Is(err, ErrHTTP)`, instead of the two-line
// `var errHTTP HTTPStatusErr; errorsAs(err, &errHTTP)`. The latter is still
// possible if the error type is required.
func (err HTTPStatusError) Unwrap() error {
	return ErrHTTPStatus
}

// SonyError captures an error returned by the Sony REST IP control protocol
// as an error returned in the payload of an HTTP response. These errors are
// returned as an error code and a string describing it.
type SonyError struct {
	Code    int
	Message string
}

// NewSonyError returns a SonyError from the response. The []any parameter
// is expected to have two elements, a float64 (code) and a string (message).
// If the size or types are not as just described, a InvalidResponseError
// is returned instead with the body that could not be parsed.
//
//nolint:goerr113 // we _are_ using wrapped errors
func NewSonyError(resp []any, body []byte) error {
	if len(resp) != 2 {
		return InvalidResponseError{
			wrapped: errors.New("wrong number of error parameters"),
			Body:    body,
		}
	}
	code, ok := resp[0].(float64) // float64 as JSON decodes numbers as float64
	if !ok {
		return InvalidResponseError{
			wrapped: errors.New("first parameter is not a number"),
			Body:    body,
		}
	}
	msg, ok := resp[1].(string)
	if !ok {
		return InvalidResponseError{
			wrapped: errors.New("second parameter is not a string"),
			Body:    body,
		}
	}
	return SonyError{Code: int(code), Message: msg}
}

// Error returns the message portion of the error.
func (err SonyError) Error() string {
	return err.Message
}

// Unwrap returns ErrSony for all errors so SonyErrors can easily be checked
// with `errors.Is(err, ErrSony)` for when you don't care about the specific
// code or message of the error (when you would use `errors.As()`).
func (err SonyError) Unwrap() error {
	return ErrSony
}

// InvalidResponseError captures a response from the TV that could not be parsed
// as expected. It wraps an error describing the error condition and the body that
// could not be parsed.
type InvalidResponseError struct {
	wrapped error
	Body    []byte
}

// Error returns the error string of the wrapped error with the body appended.
func (err InvalidResponseError) Error() string {
	return fmt.Sprintf("%v\nBody: %s", err.wrapped, string(err.Body))
}

// Unwrap returns the error that err wraps.
func (err InvalidResponseError) Unwrap() error {
	return err.wrapped
}

// NewRESTClient creates and returns a BraviaClient reachable at the given
// hostname, using the Pre-Shared Key given as psk as the password. If psk is
// the empty string, it is not used.
func NewRESTClient(hostname, psk string) *RESTClient {
	return &RESTClient{
		BaseURL: "http://" + hostname + "/sony",
		PSK:     psk,
		HTTPClient: &http.Client{
			// Timeout after 10s. Arguably that's too long.
			// This doesn't really need to be configurable.
			Timeout: 10 * time.Second,
		},
	}
}

// empty is a type to be used with `post[T]()` for when a response is not returned.
// e.g. `_, err := post[empty](...)`.
type empty struct{}

// PowerStatus returns the power status of the TV - i.e. whether it is on
// or off. On is returned as "active", off as "standby". If an error occurred
// communicating with the TV, an error is returned with an empty string status.
func (c *RESTClient) PowerStatus() (string, error) {
	type powerStatusResponse struct {
		Status string `json:"status"`
	}
	resp, err := post[powerStatusResponse](c, "system", "getPowerStatus", "1.0", nil)
	if err != nil {
		return "", err
	}
	return resp.Status, nil
}

// SetPowerStatus sets the TV power status to on (status == true) or off
// (status == false).
func (c *RESTClient) SetPowerStatus(status bool) error {
	param := map[string]bool{"status": status}
	_, err := post[empty](c, "system", "setPowerStatus", "1.0", param)
	return err
}

// SelectedInput returns the TVs currently selected input. Inputs are described
// in the form of a URI.
func (c *RESTClient) SelectedInput() (string, error) {
	type selectedInputResponse struct {
		Source string `json:"source"`
		Title  string `json:"title"`
		URI    string `json:"uri"`
	}
	selected, err := post[selectedInputResponse](c, "avContent", "getPlayingContentInfo", "1.0", nil)
	if err != nil {
		return "", err
	}
	return selected.URI, nil
}

// Inputs returns a map of all the inputs available, mapping each input's URI
// to its label, and its label to its URI if it has a label. This allows inputs
// to be looked up by either URI or label.
func (c *RESTClient) Inputs() (map[string]string, error) {
	type inputsStatusResponse struct {
		URI   string `json:"uri"`
		Label string `json:"label"`
	}
	inputs, err := post[[]inputsStatusResponse](c, "avContent", "getCurrentExternalInputsStatus", "1.0", nil)
	if err != nil {
		return nil, err
	}
	result := map[string]string{}
	for _, input := range *inputs {
		result[input.URI] = input.Label
		result[input.Label] = input.URI
	}
	return result, nil
}

// SetInput sets the current input of the TV to the given URI.
func (c *RESTClient) SetInput(uri string) error {
	param := map[string]string{"uri": uri}
	_, err := post[empty](c, "avContent", "setPlayContent", "1.0", param)
	return err
}

// post[T] executes a REST IP control command returning the result of type T or
// an error if the command did not succeed. If no data was returned from the
// HTTP call, the returned value will be nil. The `empty` type can be used when
// no response is expected:
//
//	_, err := post[empty](client, service, method, version, params)
//
// The protocol docs define service, method and version. Params is any value
// that can be marshaled as JSON and will be passed in the `params` part of the
// JSON payload of the HTTP request. Note that the method argument is not an
// HTTP method, but a method as defined in the protocol docs.
//
// The `result` field in the JSON response will be unmarshaled into a variable
// of type T and returned.
func post[T any](c *RESTClient, service, method, version string, params any) (*T, error) {
	brq, err := c.newRequest(service, method, version, params)
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	resp, err := c.do(brq)
	if err != nil {
		return nil, fmt.Errorf("http: %w", err)
	}
	bresp, err := decodeResp[T](resp)
	if err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	if len(bresp) == 0 {
		return nil, nil //nolint:nilnil // T can be `empty` for no result expected. not an error.
	}
	return &bresp[0], nil
}

func (c *RESTClient) newRequest(service, method, version string, params any) (*http.Request, error) {
	payload := struct {
		Method  string `json:"method"`
		Version string `json:"version"`
		ID      int    `json:"id"`
		Params  []any  `json:"params"`
	}{
		Method:  method,
		Version: version,
		Params:  makeParams(params),
		ID:      1, // ID 0 is invalid, but we don't care about this
	}
	u, err := url.JoinPath(c.BaseURL, service)
	if err != nil {
		return nil, fmt.Errorf("join path: %w", err)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, u, bytes.NewReader(body)) //nolint:noctx
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	if c.PSK != "" {
		req.Header.Add("X-Auth-PSK", c.PSK)
	}
	return req, nil
}

func (c *RESTClient) do(req *http.Request) (*http.Response, error) {
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close() //nolint:errcheck,gosec // When does this close ever fail meaningfully?
		return nil, HTTPStatusError(resp.StatusCode)
	}
	return resp, nil
}

func decodeResp[T any](resp *http.Response) ([]T, error) {
	defer resp.Body.Close() //nolint:errcheck // When does this close ever fail meaningfully?
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("readall: %w", err)
	}

	bresp := struct {
		Result []T   `json:"result"`
		Error  []any `json:"error"`
	}{}

	if err := json.Unmarshal(body, &bresp); err != nil {
		return nil, InvalidResponseError{
			wrapped: err,
			Body:    body,
		}
	}
	// Errors are returned like: `{"error": [40005, "Display Is Turned Off"]}`
	if bresp.Error != nil {
		return nil, NewSonyError(bresp.Error, body)
	}
	return bresp.Result, nil
}

func makeParams(v any) []any {
	if v == nil {
		return []any{}
	}
	return []any{v}
}

// Package client calls an Istok hub over pinned mutual TLS.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/vtimame/istok.sh/internal/hub/identity"
	"github.com/vtimame/istok.sh/internal/hub/protocol"
)

// Client talks to one hub as one device.
type Client struct {
	base string
	http *http.Client
}

// New connects to address (HOST:PORT) and trusts only the hub with the
// pinned fingerprint.
func New(address, hubFingerprint string, device identity.Identity) *Client {
	transport := &http.Transport{
		TLSClientConfig:     identity.ClientConfig(device, hubFingerprint),
		ForceAttemptHTTP2:   true,
		TLSHandshakeTimeout: 10 * time.Second,
		IdleConnTimeout:     time.Minute,
	}

	return &Client{
		base: "https://" + address,
		http: &http.Client{Transport: transport, Timeout: 2 * time.Minute},
	}
}

// Close drops idle connections.
func (c *Client) Close() { c.http.CloseIdleConnections() }

func (c *Client) Enroll(ctx context.Context, secret string) (protocol.EnrollResponse, error) {
	var response protocol.EnrollResponse
	err := c.call(ctx, http.MethodPost, "/v1/enroll", protocol.EnrollRequest{Secret: secret}, &response)

	return response, err
}

func (c *Client) Space(ctx context.Context) (protocol.Space, error) {
	var response protocol.Space
	err := c.call(ctx, http.MethodGet, "/v1/space", nil, &response)

	return response, err
}

func (c *Client) SetSpace(ctx context.Context, keyCheck string) error {
	return c.call(ctx, http.MethodPut, "/v1/space", protocol.Space{KeyCheck: keyCheck}, nil)
}

func (c *Client) Projects(ctx context.Context) ([]protocol.Project, error) {
	var response protocol.ProjectsResponse
	err := c.call(ctx, http.MethodGet, "/v1/projects", nil, &response)

	return response.Projects, err
}

func (c *Client) PutProject(ctx context.Context, value protocol.Project) error {
	return c.call(ctx, http.MethodPut, "/v1/projects/"+url.PathEscape(value.ID), value, nil)
}

func (c *Client) Push(ctx context.Context, request protocol.PushRequest) ([]protocol.PushResult, error) {
	var response protocol.PushResponse
	if err := c.call(ctx, http.MethodPost, "/v1/push", request, &response); err != nil {
		return nil, err
	}
	if len(response.Results) != len(request.Rows) {
		return nil, fmt.Errorf("hub answered %d of %d pushed rows", len(response.Results), len(request.Rows))
	}

	return response.Results, nil
}

func (c *Client) Pull(ctx context.Context, projectID string, after int64) (protocol.PullResponse, error) {
	query := url.Values{
		"project": {projectID},
		"after":   {strconv.FormatInt(after, 10)},
		"limit":   {strconv.Itoa(protocol.MaxPullRows)},
	}

	var response protocol.PullResponse
	err := c.call(ctx, http.MethodGet, "/v1/pull?"+query.Encode(), nil, &response)

	return response, err
}

func (c *Client) call(ctx context.Context, method, path string, body, target any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}

	request, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		return err
	}
	request.Header.Set(protocol.HeaderVersion, strconv.Itoa(protocol.Version))
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	response, err := c.http.Do(request)
	if err != nil {
		return describeTransportError(err)
	}
	defer response.Body.Close()

	limited := io.LimitReader(response.Body, protocol.MaxBodyBytes+1)
	if response.StatusCode != http.StatusOK {
		var failure protocol.Error
		if err := json.NewDecoder(limited).Decode(&failure); err != nil || failure.Message == "" {
			return fmt.Errorf("hub answered %s", response.Status)
		}
		return &failure
	}

	if target == nil {
		return nil
	}
	if err := json.NewDecoder(limited).Decode(target); err != nil {
		return fmt.Errorf("read hub response: %w", err)
	}

	return nil
}

// describeTransportError explains the handshake failures people meet.
func describeTransportError(err error) error {
	if errors.Is(err, identity.ErrUnknownPeer) {
		return fmt.Errorf("the hub's key does not match the invitation; it may have been replaced: %w", err)
	}

	return fmt.Errorf("cannot reach the hub (a revoked or unregistered device is rejected during the connection): %w", err)
}

package proxmox

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type Client struct {
	baseURL *url.URL
	token   string
	http    *http.Client
}

type Config struct {
	APIURL             string
	APIToken           string
	CAFile             string
	InsecureSkipVerify bool
}

func NewClient(cfg Config) (*Client, error) {
	base, err := url.Parse(cfg.APIURL)
	if err != nil || base.Scheme != "https" || base.Host == "" {
		return nil, errors.New("Proxmox API URL must be an absolute HTTPS URL")
	}
	base.Path = strings.TrimSuffix(base.Path, "/") + "/api2/json/"
	if !strings.Contains(cfg.APIToken, "!") || !strings.Contains(cfg.APIToken, "=") {
		return nil, errors.New("Proxmox API token must use user@realm!token=secret format")
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	if cfg.CAFile != "" {
		pem, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, fmt.Errorf("read Proxmox CA: %w", err)
		}
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, errors.New("Proxmox CA file contains no valid certificates")
		}
		tlsConfig.RootCAs = pool
	}
	// Insecure mode is rejected by config.Load in production. It remains
	// available only for explicitly isolated development smoke tests.
	tlsConfig.InsecureSkipVerify = cfg.InsecureSkipVerify //nolint:gosec
	return &Client{
		baseURL: base,
		token:   cfg.APIToken,
		http: &http.Client{Timeout: 20 * time.Second, Transport: &http.Transport{
			TLSClientConfig: tlsConfig, MaxIdleConns: 10, IdleConnTimeout: 60 * time.Second,
		}},
	}, nil
}

type Version struct {
	Version string `json:"version"`
	Release string `json:"release"`
	RepoID  string `json:"repoid"`
}

type Node struct {
	Node      string  `json:"node"`
	Status    string  `json:"status"`
	CPU       float64 `json:"cpu"`
	MaxCPU    int     `json:"maxcpu"`
	Memory    int64   `json:"mem"`
	MaxMemory int64   `json:"maxmem"`
	Uptime    int64   `json:"uptime"`
}

type Resource struct {
	ID        string `json:"id"`
	VMID      int    `json:"vmid"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	Node      string `json:"node"`
	Status    string `json:"status"`
	Template  int    `json:"template"`
	Pool      string `json:"pool,omitempty"`
	MaxCPU    int    `json:"maxcpu"`
	MaxMemory int64  `json:"maxmem"`
	MaxDisk   int64  `json:"maxdisk"`
}

type Storage struct {
	Storage string `json:"storage"`
	Node    string `json:"node"`
	Type    string `json:"type"`
	Content string `json:"content"`
	Active  int    `json:"active"`
	Enabled int    `json:"enabled"`
	Shared  int    `json:"shared"`
	Total   int64  `json:"total"`
	Used    int64  `json:"used"`
	Avail   int64  `json:"avail"`
}

type Network struct {
	Iface   string `json:"iface"`
	Node    string `json:"node"`
	Type    string `json:"type"`
	Active  int    `json:"active"`
	Bridge  string `json:"bridge_ports,omitempty"`
	Address string `json:"address,omitempty"`
	CIDR    string `json:"cidr,omitempty"`
	Gateway string `json:"gateway,omitempty"`
}

type Pool struct {
	PoolID  string `json:"poolid"`
	Comment string `json:"comment,omitempty"`
}

type Inventory struct {
	CollectedAt time.Time  `json:"collectedAt"`
	Version     Version    `json:"version"`
	Nodes       []Node     `json:"nodes"`
	Resources   []Resource `json:"resources"`
	Storage     []Storage  `json:"storage"`
	Networks    []Network  `json:"networks"`
	Pools       []Pool     `json:"pools"`
}

func (c *Client) Inventory(ctx context.Context) (Inventory, error) {
	var inventory Inventory
	if err := c.get(ctx, "version", &inventory.Version); err != nil {
		return inventory, fmt.Errorf("read Proxmox version: %w", err)
	}
	if err := c.get(ctx, "nodes", &inventory.Nodes); err != nil {
		return inventory, fmt.Errorf("read Proxmox nodes: %w", err)
	}
	if err := c.get(ctx, "cluster/resources?type=vm", &inventory.Resources); err != nil {
		return inventory, fmt.Errorf("read Proxmox resources: %w", err)
	}
	if err := c.get(ctx, "pools", &inventory.Pools); err != nil {
		return inventory, fmt.Errorf("read Proxmox pools: %w", err)
	}
	for _, node := range inventory.Nodes {
		var storage []Storage
		if err := c.get(ctx, "nodes/"+url.PathEscape(node.Node)+"/storage", &storage); err != nil {
			return inventory, fmt.Errorf("read storage on node %s: %w", node.Node, err)
		}
		for index := range storage {
			storage[index].Node = node.Node
		}
		inventory.Storage = append(inventory.Storage, storage...)
		var networks []Network
		if err := c.get(ctx, "nodes/"+url.PathEscape(node.Node)+"/network", &networks); err != nil {
			return inventory, fmt.Errorf("read networks on node %s: %w", node.Node, err)
		}
		for index := range networks {
			networks[index].Node = node.Node
		}
		inventory.Networks = append(inventory.Networks, networks...)
	}
	inventory.CollectedAt = time.Now().UTC()
	return inventory, nil
}

func (c *Client) get(ctx context.Context, path string, target any) error {
	reference, err := url.Parse(path)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL.ResolveReference(reference).String(), nil)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "PVEAPIToken="+c.token)
	request.Header.Set("Accept", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	body := io.LimitReader(response.Body, 16*1024*1024)
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, body)
		switch response.StatusCode {
		case http.StatusUnauthorized:
			return errors.New("authentication failed: check token ID and secret")
		case http.StatusForbidden:
			return errors.New("permission denied: grant PVE Auditor to the token")
		default:
			return fmt.Errorf("unexpected HTTP status %d", response.StatusCode)
		}
	}
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(body).Decode(&envelope); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	if len(envelope.Data) == 0 {
		return errors.New("response contains no data")
	}
	if err := json.Unmarshal(envelope.Data, target); err != nil {
		return fmt.Errorf("decode data: %w", err)
	}
	return nil
}

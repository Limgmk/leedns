package dns

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"time"

	D "github.com/miekg/dns"
)

const (
	DOHMSGMIMETYPE  = "application/dns-message"
	DOHJSONMIMETYPE = "application/dns-json"
)

type Client interface {
	Exchange(m *D.Msg) (msg *D.Msg, rtt time.Duration, err error)
	ExchangeContext(ctx context.Context, m *D.Msg) (msg *D.Msg, rtt time.Duration, err error)
}

type generalClient struct {
	*D.Client
	host string
	port string
}

type httpClient struct {
	url       string
	transport *http.Transport
}

type Resolver interface {
	ResolveHost(host string) (ip net.IP, err error)
}

type defaultResolve struct{}

func (r *defaultResolve) ResolveHost(host string) (ip net.IP, err error) {

	ip = net.ParseIP(host)
	if ip != nil {
		return ip, nil
	}

	ips, err := net.LookupIP(host)
	if err != nil {
		return nil, err
	}

	num := len(ips)
	if num == 0 {
		return nil, errors.New("couldn't find ip")
	}

	ip = ips[rand.Intn(num)]

	return ip, nil
}

var resolver Resolver = new(defaultResolve)

func SetResolver(r Resolver) {
	resolver = r
}

func (c *generalClient) Exchange(m *D.Msg) (result *D.Msg, rtt time.Duration, err error) {
	return c.ExchangeContext(context.Background(), m)
}

func (c *generalClient) ExchangeContext(ctx context.Context, m *D.Msg) (msg *D.Msg, rtt time.Duration, err error) {
	var ip net.IP

	ip, err = resolver.ResolveHost(c.host)
	if err != nil {
		return nil, 0, fmt.Errorf("resolve nameserver host failed: %w", err)
	}

	type result struct {
		msg *D.Msg
		rtt time.Duration
		err error
	}

	ch := make(chan result)
	go func() {
		msg, rtt, err = c.Client.Exchange(m, net.JoinHostPort(ip.String(), c.port))
		ch <- result{msg, rtt, err}
	}()

	select {
	case <-ctx.Done():
		return nil, 0, ctx.Err()
	case ret := <-ch:
		return ret.msg, ret.rtt, ret.err
	}
}

func (dc *httpClient) Exchange(m *D.Msg) (msg *D.Msg, rtt time.Duration, err error) {
	return dc.ExchangeContext(context.Background(), m)
}

func (dc *httpClient) ExchangeContext(ctx context.Context, m *D.Msg) (msg *D.Msg, rtt time.Duration, err error) {
	req, err := dc.newRequest(m)
	if err != nil {
		return nil, 0, err
	}

	req = req.WithContext(ctx)

	return dc.doRequest(req)
}

func (dc *httpClient) newRequest(m *D.Msg) (*http.Request, error) {
	buf, err := m.Pack()
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPost, dc.url, bytes.NewReader(buf))
	if err != nil {
		return req, err
	}

	req.Header.Set("content-type", DOHMSGMIMETYPE)
	req.Header.Set("accept", DOHMSGMIMETYPE)
	return req, nil
}

func (dc *httpClient) doRequest(req *http.Request) (msg *D.Msg, rtt time.Duration, err error) {
	client := &http.Client{Transport: dc.transport}

	t := time.Now()
	resp, err := client.Do(req)
	rtt = time.Since(t)
	if err != nil {
		return nil, rtt, err
	}

	defer func() {
		err := resp.Body.Close()
		if err != nil {
			log.Println(err)
		}
	}()

	buf, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, rtt, err
	}
	msg = new(D.Msg)
	err = msg.Unpack(buf)
	return msg, rtt, err
}

func newHTTPClient(url string) *httpClient {
	return &httpClient{
		url: url,
		transport: &http.Transport{
			TLSClientConfig:   &tls.Config{},
			ForceAttemptHTTP2: true,
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				host, port, err := net.SplitHostPort(addr)
				if err != nil {
					return nil, err
				}

				ip, err := resolver.ResolveHost(host)
				if err != nil {
					return nil, fmt.Errorf("resolve nameserver host failed: %w", err)
				}

				return net.Dial("tcp", net.JoinHostPort(ip.String(), port))
			},
		},
	}
}

func newGeneralClient(addr string) *generalClient {
	parse, err := url.Parse(addr)
	if err != nil {
		log.Println(err.Error())
		return nil
	}
	scheme, host, port := parse.Scheme, parse.Hostname(), parse.Port()
	if scheme == "tls" {
		scheme = "tcp-tls"
	}
	c := &generalClient{
		Client: &D.Client{
			Net:       scheme,
			TLSConfig: &tls.Config{ServerName: host},
			UDPSize:   4096,
			Timeout:   time.Second * 5,
		},
		port: port,
		host: host,
	}
	if c.port == "" {
		switch scheme {
		case "udp":
			c.port = "53"
		case "tcp":
			c.port = "53"
		case "tcp-tls":
			c.port = "853"
		}
	}
	return c
}

func NewClient(addr string) (c Client, err error) {
	parse, err := url.Parse(addr)
	if err != nil {
		return nil, err
	}

	if resolver == nil {
		resolver = new(defaultResolve)
	}

	switch parse.Scheme {
	case "http", "https":
		return newHTTPClient(addr), nil
	default:
		return newGeneralClient(addr), nil
	}
}

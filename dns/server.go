package dns

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"strings"

	"github.com/libp2p/go-reuseport"
	jsonDNS "github.com/m13253/dns-over-https/json-dns"
	D "github.com/miekg/dns"
)

type remote struct {
	Addr string
}

type Query struct {
	Msg    *D.Msg
	Remote *remote
}

type ResponseWriter interface {
	Write([]byte) (int, error)
	WriteMsg(*D.Msg) error
}

type generalResponseWriter struct {
	D.ResponseWriter
}

type httpResponseWriter struct {
	http.ResponseWriter
	r *http.Request
}

type Handler interface {
	ServeDNS(w ResponseWriter, q *Query)
}

type generalHandler struct {
	h Handler
	D.Handler
}

type httpHandler struct {
	h Handler
	http.Handler
}

func (h generalHandler) ServeDNS(w D.ResponseWriter, r *D.Msg) {
	ww := new(generalResponseWriter)
	ww.ResponseWriter = w

	q := &Query{
		Msg: r,
		Remote: &remote{
			Addr: w.RemoteAddr().String(),
		},
	}
	h.h.ServeDNS(ww, q)
}

func (h httpHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	buf, err := ioutil.ReadAll(r.Body)
	if err != nil {
		log.Println(err.Error())
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	params := r.URL.Query()

	m := new(D.Msg)
	if err = D.IsMsg(buf); err != nil {
		domainName := params.Get("name")
		dnsType := params.Get("type")
		if domainName == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if !strings.HasSuffix(domainName, ".") {
			domainName += "."
		}
		if dnsType == "" {
			dnsType = "A"
		}
		m.SetQuestion(domainName, D.StringToType[dnsType])
	} else {
		err = m.Unpack(buf)
		if err != nil {
			w.WriteHeader(http.StatusUnsupportedMediaType)
			return
		}
	}

	var ct string

	ct = params.Get("ct")

	if ct == "" {
		ct = r.Header.Get("Accept")
	}

	if ct != DOHJSONMIMETYPE && ct != DOHMSGMIMETYPE {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-type", ct)

	ww := new(httpResponseWriter)
	ww.ResponseWriter = w
	ww.r = r

	q := &Query{
		Msg: m,
		Remote: &remote{
			Addr: r.RemoteAddr,
		},
	}

	h.h.ServeDNS(ww, q)
}

func (w httpResponseWriter) WriteMsg(m *D.Msg) error {
	var msg []byte
	var err error

	if w.Header().Get("Content-type") == DOHJSONMIMETYPE {
		jsonMsg := jsonDNS.Marshal(m)
		msg, err = json.Marshal(jsonMsg)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return err
		}
	} else {
		msg, err = m.Pack()
		if err != nil {
			return err
		}
	}

	_, err = w.ResponseWriter.Write(msg)
	return err
}

func ListenAndServe(addr, network string, handler Handler) error {
	var srv = new(D.Server)

	switch network {
	case "udp":
		pkt, err := reuseport.ListenPacket(network, addr)
		if err != nil {
			return err
		}
		srv.PacketConn = pkt
	case "tcp":
		lsn, err := reuseport.Listen(network, addr)
		if err != nil {
			return err
		}
		srv.Listener = lsn
	default:
		return fmt.Errorf("error network type: %s", network)
	}

	h := new(generalHandler)
	h.h = handler
	srv.Handler = h

	go func() {
		log.Println(srv.ActivateAndServe())
	}()

	return nil
}

func loadCertFile(certFile, keyFile string) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, err
	}

	return &tls.Config{Certificates: []tls.Certificate{cert}}, nil
}

func ListenAndServeTLS(addr, certFile, keyFile string, handler Handler) error {
	var srv = new(D.Server)

	tlsConfig, err := loadCertFile(certFile, keyFile)
	if err != nil {
		return err
	}

	lsn, err := reuseport.Listen("tcp", addr)
	if err != nil {
		return err
	}

	lsn = tls.NewListener(lsn, tlsConfig)

	srv.Listener = lsn

	h := new(generalHandler)
	h.h = handler
	srv.Handler = h

	go func() {
		log.Println(srv.ActivateAndServe())
	}()

	return nil
}

func newHTTPServer(pattern string, handler Handler) *http.Server {
	var srv = new(http.Server)

	h := new(httpHandler)
	h.h = handler

	router := http.NewServeMux()
	if pattern == "" {
		pattern = "/dns-query"
	}
	router.Handle(pattern, h)
	srv.Handler = router

	return srv
}

func ListenHTTPAndServe(addr, pattern string, handler Handler) error {

	lsn, err := reuseport.Listen("tcp", addr)
	if err != nil {
		return err
	}

	go func() {
		log.Println(newHTTPServer(pattern, handler).Serve(lsn))
	}()

	return nil
}

func ListenHTTPAndServeTLS(addr, pattern, certFile, keyFile string, handler Handler) error {

	tlsConfig, err := loadCertFile(certFile, keyFile)
	if err != nil {
		return err
	}

	lsn, err := reuseport.Listen("tcp", addr)
	if err != nil {
		return err
	}

	lsn = tls.NewListener(lsn, tlsConfig)

	go func() {
		log.Println(newHTTPServer(pattern, handler).Serve(lsn))
	}()

	return nil
}
